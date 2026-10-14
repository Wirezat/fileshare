package office

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSecret = "06e7f94b2b06ce5c7fc22730c5639b6eb1cab0a36725a9648e6396546ae2fdca"

func TestDocType(t *testing.T) {
	cases := map[string]string{
		"bericht.docx": "word", "alt.doc": "word", "brief.odt": "word",
		"zahlen.xlsx": "cell", "alt.xls": "cell", "tabelle.ods": "cell",
		"folien.pptx": "slide", "alt.ppt": "slide", "vortrag.odp": "slide",
		"GROSS.DOCX": "word",
		"scan.pdf":   "", "notiz.txt": "", "bild.png": "", "ohne-endung": "",
	}
	for name, want := range cases {
		if got := DocType(name); got != want {
			t.Errorf("DocType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestJWTRoundTrip(t *testing.T) {
	claims := map[string]any{"document": map[string]any{"key": "abc"}, "mode": "view"}
	tok, err := SignJWT(testSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("token has %d dots, want 2: %s", strings.Count(tok, "."), tok)
	}

	var got map[string]any
	if err := VerifyJWT(testSecret, tok, &got); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got["mode"] != "view" {
		t.Errorf("payload lost: %v", got)
	}
}

func TestJWTRejectsTampering(t *testing.T) {
	tok, _ := SignJWT(testSecret, map[string]any{"mode": "view"})
	parts := strings.Split(tok, ".")

	forged, _ := SignJWT(testSecret, map[string]any{"mode": "edit"})
	forgedPayload := strings.Split(forged, ".")[1]

	cases := map[string]string{
		"payload swapped":  parts[0] + "." + forgedPayload + "." + parts[2],
		"signature broken": parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-2] + "xx",
		"too few parts":    parts[0] + "." + parts[1],
		"empty":            "",
	}
	for name, bad := range cases {
		var out map[string]any
		if err := VerifyJWT(testSecret, bad, &out); err == nil {
			t.Errorf("%s: verified, want error", name)
		}
	}

	var out map[string]any
	if err := VerifyJWT("other-secret", tok, &out); err == nil {
		t.Error("wrong secret: verified, want error")
	}
	if err := VerifyJWT("", tok, &out); err == nil {
		t.Error("empty secret: verified, want error")
	}
}

func TestJWTRejectsAlgNone(t *testing.T) {
	header := b64url([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64url([]byte(`{"mode":"edit"}`))
	var out map[string]any
	if err := VerifyJWT(testSecret, header+"."+payload+".", &out); err == nil {
		t.Error("alg=none accepted")
	}
}

func TestDocKeyFollowsTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.docx")
	os.WriteFile(p, []byte("one"), 0o600)
	info1, _ := os.Stat(p)
	k1 := DocKey(p, info1)
	k1again := DocKey(p, info1)

	os.WriteFile(p, []byte("one two"), 0o600)
	os.Chtimes(p, time.Now(), time.Now().Add(time.Hour))
	info2, _ := os.Stat(p)
	k2 := DocKey(p, info2)

	if k1 != k1again {
		t.Error("key is not stable for an unchanged file")
	}
	if k1 == k2 {
		t.Error("key did not change after the file changed")
	}
	if len(k1) == 0 || len(k1) > 128 {
		t.Errorf("key length %d outside 1..128", len(k1))
	}
	for _, c := range k1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("key contains %q, want hex only", c)
			break
		}
	}
}

func bearer(t *testing.T, secret string, claims map[string]any) string {
	t.Helper()
	tok, err := SignJWT(secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func urlBearer(t *testing.T, secret, url string, exp int64) string {
	t.Helper()
	return bearer(t, secret, map[string]any{
		"payload": map[string]any{"url": url},
		"iat":     time.Now().Unix(),
		"exp":     exp,
	})
}

func TestTokenAllows(t *testing.T) {
	future := time.Now().Add(5 * time.Minute).Unix()
	past := time.Now().Add(-time.Minute).Unix()
	target := "/docs/bericht.docx?dl=1"

	cases := []struct {
		name   string
		header string
		secret string
		want   bool
	}{
		{"matching url", urlBearer(t, testSecret, "https://fs.example.com"+target, future), testSecret, true},
		{"other host, same path", urlBearer(t, testSecret, "http://host.containers.internal:27182"+target, future), testSecret, true},
		{"other file", urlBearer(t, testSecret, "https://fs.example.com/docs/geheim.docx?dl=1", future), testSecret, false},
		{"without dl", urlBearer(t, testSecret, "https://fs.example.com/docs/bericht.docx", future), testSecret, false},
		{"expired", urlBearer(t, testSecret, "https://fs.example.com"+target, past), testSecret, false},
		{"wrong secret", urlBearer(t, "nope", "https://fs.example.com"+target, future), testSecret, false},
		{"no secret configured", urlBearer(t, testSecret, "https://fs.example.com"+target, future), "", false},
		{"no header", "", testSecret, false},
		{"not bearer", "Basic abc", testSecret, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TokenAllows(tc.header, target, tc.secret); got != tc.want {
				t.Errorf("TokenAllows = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEditorConfigIsSignedAndReadOnly(t *testing.T) {
	cfg := EditorConfig(Doc{
		Name: "bericht.docx", Key: "k1", URL: "https://fs.example.com/docs/bericht.docx?dl=1",
	}, "de", testSecret)

	tok, _ := cfg["token"].(string)
	if tok == "" {
		t.Fatal("config carries no token")
	}
	delete(cfg, "token")

	var signed map[string]any
	if err := VerifyJWT(testSecret, tok, &signed); err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	want, _ := json.Marshal(cfg)
	got, _ := json.Marshal(signed)
	if string(want) != string(got) {
		t.Errorf("token signs something other than the config:\n%s\n%s", want, got)
	}

	doc := cfg["document"].(map[string]any)
	if doc["fileType"] != "docx" || doc["title"] != "bericht.docx" || doc["key"] != "k1" {
		t.Errorf("document block wrong: %v", doc)
	}
	if cfg["documentType"] != "word" {
		t.Errorf("documentType = %v", cfg["documentType"])
	}
	ec := cfg["editorConfig"].(map[string]any)
	if ec["mode"] != "view" || ec["lang"] != "de" {
		t.Errorf("editorConfig = %v", ec)
	}
	if _, ok := ec["callbackUrl"]; ok {
		t.Error("view mode carries a callbackUrl")
	}
	perms := doc["permissions"].(map[string]any)
	if perms["edit"] != false {
		t.Errorf("permissions.edit = %v, want false", perms["edit"])
	}
}

func TestEditorConfigEditModeCarriesCallback(t *testing.T) {
	cfg := EditorConfig(Doc{Name: "b.docx", Key: "k", URL: "u", Edit: true, Callback: "https://fs/cb"}, "en", testSecret)
	ec := cfg["editorConfig"].(map[string]any)
	if ec["mode"] != "edit" || ec["callbackUrl"] != "https://fs/cb" {
		t.Errorf("editorConfig = %v", ec)
	}
}

func TestOpensInline(t *testing.T) {
	cases := []struct {
		enabled bool
		name    string
		want    bool
	}{
		{true, "bericht.docx", true},
		{false, "bericht.docx", false},
		{true, "scan.pdf", true},
		{false, "scan.pdf", true},
		{true, "notiz.txt", false},
		{true, "archiv.zip", false},
	}
	for _, tc := range cases {
		if got := OpensInline(tc.enabled, tc.name); got != tc.want {
			t.Errorf("OpensInline(%v, %q) = %v, want %v", tc.enabled, tc.name, got, tc.want)
		}
	}
}

func TestParseCallback(t *testing.T) {
	cb := map[string]any{"key": "k", "status": 2, "url": "https://ds/cache/x"}
	bodyTok, _ := SignJWT(testSecret, cb)
	body, _ := json.Marshal(map[string]any{"token": bodyTok})
	header := bearer(t, testSecret, map[string]any{"payload": cb})

	cases := []struct {
		name   string
		header string
		body   []byte
		secret string
		want   bool
	}{
		{"bearer header", header, []byte(`{}`), testSecret, true},
		{"token in body", "", body, testSecret, true},
		{"bad header, good body", "Bearer junk", body, testSecret, true},
		{"wrong secret", header, body, "nope", false},
		{"nothing signed", "", []byte(`{"key":"k","status":2,"url":"https://evil/x"}`), testSecret, false},
		{"not json", "", []byte("garbage"), testSecret, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseCallback(tc.header, tc.body, tc.secret)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok && (got.Key != "k" || got.Status != 2 || got.URL != "https://ds/cache/x") {
				t.Errorf("callback = %+v", got)
			}
		})
	}
}

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestReplaceFromURLKeepsModeAndFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.docx")
	os.WriteFile(real, []byte("old"), 0o640)
	link := filepath.Join(dir, "link.docx")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFromURL(link, serve(t, http.StatusOK, "new"), 0); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(real)
	info, _ := os.Stat(real)
	if string(got) != "new" || info.Mode().Perm() != 0o640 {
		t.Errorf("real file = %q mode %v", got, info.Mode().Perm())
	}
	if l, err := os.Lstat(link); err != nil || l.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a regular file")
	}
}

func TestReplaceFromURLRefusesAndKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.docx")
	os.WriteFile(p, []byte("old"), 0o600)
	cases := map[string]struct {
		path, url string
		limit     int64
	}{
		"too large":  {p, serve(t, http.StatusOK, "0123456789"), 5},
		"not 200":    {p, serve(t, http.StatusNotFound, "x"), 0},
		"directory":  {dir, serve(t, http.StatusOK, "x"), 0},
		"no such fn": {filepath.Join(dir, "missing.docx"), serve(t, http.StatusOK, "x"), 0},
	}
	for name, tc := range cases {
		if err := ReplaceFromURL(tc.path, tc.url, tc.limit); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("file changed to %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".office-*.tmp")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}
