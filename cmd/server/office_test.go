package main

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

func TestOfficeDocType(t *testing.T) {
	cases := map[string]string{
		"bericht.docx": "word", "alt.doc": "word", "brief.odt": "word",
		"zahlen.xlsx": "cell", "alt.xls": "cell", "tabelle.ods": "cell",
		"folien.pptx": "slide", "alt.ppt": "slide", "vortrag.odp": "slide",
		"GROSS.DOCX": "word",
		"scan.pdf":   "", "notiz.txt": "", "bild.png": "", "ohne-endung": "",
	}
	for name, want := range cases {
		if got := officeDocType(name); got != want {
			t.Errorf("officeDocType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestJWTRoundTrip(t *testing.T) {
	claims := map[string]any{"document": map[string]any{"key": "abc"}, "mode": "view"}
	tok, err := signJWT(testSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("token has %d dots, want 2: %s", strings.Count(tok, "."), tok)
	}

	var got map[string]any
	if err := verifyJWT(testSecret, tok, &got); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got["mode"] != "view" {
		t.Errorf("payload lost: %v", got)
	}
}

func TestJWTRejectsTampering(t *testing.T) {
	tok, _ := signJWT(testSecret, map[string]any{"mode": "view"})
	parts := strings.Split(tok, ".")

	forged, _ := signJWT(testSecret, map[string]any{"mode": "edit"})
	forgedPayload := strings.Split(forged, ".")[1]

	cases := map[string]string{
		"payload swapped":  parts[0] + "." + forgedPayload + "." + parts[2],
		"signature broken": parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-2] + "xx",
		"too few parts":    parts[0] + "." + parts[1],
		"empty":            "",
	}
	for name, bad := range cases {
		var out map[string]any
		if err := verifyJWT(testSecret, bad, &out); err == nil {
			t.Errorf("%s: verified, want error", name)
		}
	}

	var out map[string]any
	if err := verifyJWT("other-secret", tok, &out); err == nil {
		t.Error("wrong secret: verified, want error")
	}
	if err := verifyJWT("", tok, &out); err == nil {
		t.Error("empty secret: verified, want error")
	}
}

func TestJWTRejectsAlgNone(t *testing.T) {
	header := b64url([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64url([]byte(`{"mode":"edit"}`))
	var out map[string]any
	if err := verifyJWT(testSecret, header+"."+payload+".", &out); err == nil {
		t.Error("alg=none accepted")
	}
}

func TestOfficeDocKeyFollowsTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.docx")
	os.WriteFile(p, []byte("one"), 0o600)
	info1, _ := os.Stat(p)
	k1 := officeDocKey(p, info1)
	k1again := officeDocKey(p, info1)

	os.WriteFile(p, []byte("one two"), 0o600)
	os.Chtimes(p, time.Now(), time.Now().Add(time.Hour))
	info2, _ := os.Stat(p)
	k2 := officeDocKey(p, info2)

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

func dsBearer(t *testing.T, secret, url string, exp int64) string {
	t.Helper()
	claims := map[string]any{
		"payload": map[string]any{"url": url},
		"iat":     time.Now().Unix(),
		"exp":     exp,
	}
	tok, err := signJWT(secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func TestDSTokenAllows(t *testing.T) {
	future := time.Now().Add(5 * time.Minute).Unix()
	past := time.Now().Add(-time.Minute).Unix()
	target := "/docs/bericht.docx?dl=1"

	cases := []struct {
		name   string
		header string
		secret string
		want   bool
	}{
		{"matching url", dsBearer(t, testSecret, "https://fs.example.com"+target, future), testSecret, true},
		{"other host, same path", dsBearer(t, testSecret, "http://host.containers.internal:27182"+target, future), testSecret, true},
		{"other file", dsBearer(t, testSecret, "https://fs.example.com/docs/geheim.docx?dl=1", future), testSecret, false},
		{"without dl", dsBearer(t, testSecret, "https://fs.example.com/docs/bericht.docx", future), testSecret, false},
		{"expired", dsBearer(t, testSecret, "https://fs.example.com"+target, past), testSecret, false},
		{"wrong secret", dsBearer(t, "nope", "https://fs.example.com"+target, future), testSecret, false},
		{"no secret configured", dsBearer(t, testSecret, "https://fs.example.com"+target, future), "", false},
		{"no header", "", testSecret, false},
		{"not bearer", "Basic abc", testSecret, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if got := dsTokenAllows(req, tc.secret); got != tc.want {
				t.Errorf("dsTokenAllows = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOfficeConfigIsSignedAndReadOnly(t *testing.T) {
	cfg := officeConfig(officeDoc{
		name: "bericht.docx", key: "k1", url: "https://fs.example.com/docs/bericht.docx?dl=1",
	}, "de", testSecret)

	tok, _ := cfg["token"].(string)
	if tok == "" {
		t.Fatal("config carries no token")
	}
	delete(cfg, "token")

	var signed map[string]any
	if err := verifyJWT(testSecret, tok, &signed); err != nil {
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
	perms := doc["permissions"].(map[string]any)
	if perms["edit"] != false {
		t.Errorf("permissions.edit = %v, want false", perms["edit"])
	}
}

func TestOpensInline(t *testing.T) {
	cases := []struct {
		office bool
		name   string
		want   bool
	}{
		{true, "bericht.docx", true},
		{false, "bericht.docx", false},
		{true, "scan.pdf", true},
		{false, "scan.pdf", true},
		{true, "notiz.txt", false},
		{true, "archiv.zip", false},
	}
	for _, tc := range cases {
		if got := opensInline(tc.office, tc.name); got != tc.want {
			t.Errorf("opensInline(%v, %q) = %v, want %v", tc.office, tc.name, got, tc.want)
		}
	}
}
