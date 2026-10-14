package office

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var docTypes = map[string]string{
	"docx": "word", "doc": "word", "odt": "word",
	"xlsx": "cell", "xls": "cell", "ods": "cell",
	"pptx": "slide", "ppt": "slide", "odp": "slide",
}

// Ext returns the lower-case extension of name without the dot.
func Ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// DocType returns the document server's type for name ("word", "cell", "slide") or "" if it handles none.
func DocType(name string) string {
	return docTypes[Ext(name)]
}

// OpensInline reports whether clicking name shows it instead of saving it:
// PDFs always, office documents when office is enabled for the share.
func OpensInline(enabled bool, name string) bool {
	return Ext(name) == "pdf" || (enabled && DocType(name) != "")
}

// DocKey identifies one revision of a file; it changes with the file's size or modification time.
func DocKey(path string, info os.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(sum[:20])
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// SignJWT returns an HS256 JWT whose payload is claims serialised as JSON.
func SignJWT(secret string, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	head := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(head))
	return head + "." + b64url(mac.Sum(nil)), nil
}

// VerifyJWT checks an HS256 signature and unmarshals the payload into out.
func VerifyJWT(secret, token string, out any) error {
	if secret == "" {
		return errors.New("no secret")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("malformed token")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if hb, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil || json.Unmarshal(hb, &header) != nil || header.Alg != "HS256" {
		return errors.New("unsupported token header")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

// TokenAllows reports whether authHeader carries the document server's own
// unexpired bearer token for exactly requestURI.
func TokenAllows(authHeader, requestURI, secret string) bool {
	tok, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return false
	}
	var claims struct {
		Payload struct {
			URL string `json:"url"`
		} `json:"payload"`
		Exp int64 `json:"exp"`
	}
	if err := VerifyJWT(secret, tok, &claims); err != nil {
		return false
	}
	if claims.Exp != 0 && time.Now().Unix() > claims.Exp {
		return false
	}
	u, err := url.Parse(claims.Payload.URL)
	if err != nil {
		return false
	}
	return u.RequestURI() == requestURI
}

// Doc describes the file an editor opens; Callback is used only when Edit is set.
type Doc struct {
	Name     string
	Key      string
	URL      string
	Edit     bool
	Callback string
}

// EditorConfig builds the signed editor configuration for doc.
func EditorConfig(doc Doc, lang, secret string) map[string]any {
	mode := "view"
	if doc.Edit {
		mode = "edit"
	}
	editor := map[string]any{"mode": mode, "lang": lang}
	if doc.Edit {
		editor["callbackUrl"] = doc.Callback
	}
	cfg := map[string]any{
		"document": map[string]any{
			"fileType": Ext(doc.Name),
			"key":      doc.Key,
			"title":    doc.Name,
			"url":      doc.URL,
			"permissions": map[string]any{
				"edit":     doc.Edit,
				"download": true,
				"print":    true,
			},
		},
		"documentType": DocType(doc.Name),
		"editorConfig": editor,
	}
	if tok, err := SignJWT(secret, cfg); err == nil {
		cfg["token"] = tok
	}
	return cfg
}

// Callback is the document server's save notification.
type Callback struct {
	Key    string `json:"key"`
	Status int    `json:"status"`
	URL    string `json:"url"`
}

// ParseCallback returns the verified callback from the bearer header's payload
// or from the body's own token field, and false if neither verifies.
func ParseCallback(authHeader string, body []byte, secret string) (Callback, bool) {
	var cb Callback
	if tok, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		var claims struct {
			Payload Callback `json:"payload"`
		}
		if VerifyJWT(secret, tok, &claims) == nil {
			return claims.Payload, true
		}
	}
	var wrapped struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &wrapped) == nil && wrapped.Token != "" && VerifyJWT(secret, wrapped.Token, &cb) == nil {
		return cb, true
	}
	return cb, false
}

// ReplaceFromURL downloads url and swaps it in for the file at path, keeping
// its mode and writing through any symlink; limit caps the size, 0 means 1 GiB.
func ReplaceFromURL(path, url string, limit int64) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("save target is a directory")
	}
	if limit <= 0 {
		limit = 1 << 30
	}

	client := http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("document server answered %s", resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".office-*.tmp")
	if err != nil {
		return err
	}
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, limit+1))
	if err == nil && n > limit {
		err = fmt.Errorf("saved document exceeds %d bytes", limit)
	}
	if err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
