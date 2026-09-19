package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

var officeHtmlPath = "./web/html/office.html"

var officeTypes = map[string]string{
	"docx": "word", "doc": "word", "odt": "word",
	"xlsx": "cell", "xls": "cell", "ods": "cell",
	"pptx": "slide", "ppt": "slide", "odp": "slide",
}

// officeDocType returns the document server's document type for a file name
// ("word", "cell", "slide") or "" for anything it does not handle.
func officeDocType(name string) string {
	return officeTypes[officeExt(name)]
}

func officeExt(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// opensInline reports whether clicking a file shows it instead of saving it:
// PDFs always, office documents when the share has office enabled.
func opensInline(office bool, name string) bool {
	return officeExt(name) == "pdf" || (office && officeDocType(name) != "")
}

// officeDocKey identifies one revision of a file to the document server; it
// changes whenever the file's size or modification time does.
func officeDocKey(path string, info os.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(sum[:20])
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// signJWT returns an HS256 JWT whose payload is claims serialised as JSON.
func signJWT(secret string, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	head := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + b64url(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(head))
	return head + "." + b64url(mac.Sum(nil)), nil
}

// verifyJWT checks an HS256 signature and unmarshals the payload into out.
func verifyJWT(secret, token string, out any) error {
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

// dsTokenAllows reports whether the request carries the document server's own
// bearer token for exactly this path and query.
func dsTokenAllows(r *http.Request, secret string) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	var claims struct {
		Payload struct {
			URL string `json:"url"`
		} `json:"payload"`
		Exp int64 `json:"exp"`
	}
	if err := verifyJWT(secret, tok, &claims); err != nil {
		return false
	}
	if claims.Exp != 0 && time.Now().Unix() > claims.Exp {
		return false
	}
	u, err := url.Parse(claims.Payload.URL)
	if err != nil {
		return false
	}
	return u.RequestURI() == r.URL.RequestURI()
}

type officeDoc struct {
	name string
	key  string
	url  string
}

// officeConfig builds the signed editor configuration for a read-only view.
func officeConfig(doc officeDoc, lang, secret string) map[string]any {
	cfg := map[string]any{
		"document": map[string]any{
			"fileType": officeExt(doc.name),
			"key":      doc.key,
			"title":    doc.name,
			"url":      doc.url,
			"permissions": map[string]any{
				"edit":     false,
				"download": true,
				"print":    true,
			},
		},
		"documentType": officeDocType(doc.name),
		"editorConfig": map[string]any{
			"mode": "view",
			"lang": lang,
		},
	}
	if tok, err := signJWT(secret, cfg); err == nil {
		cfg["token"] = tok
	}
	return cfg
}

// officeWanted reports whether this request should get the viewer page rather
// than the file itself.
func officeWanted(r *http.Request, ctx *requestContext) bool {
	return !ctx.fileInfo.IsDir() &&
		ctx.fileData.Office != shared.OfficeOff &&
		ctx.config.OfficeURL != "" && ctx.config.OfficeSecret != "" &&
		officeDocType(ctx.fileInfo.Name()) != "" &&
		r.URL.Query().Get("dl") == ""
}

func requestLang(r *http.Request) string {
	al := strings.ToLower(r.Header.Get("Accept-Language"))
	if strings.HasPrefix(al, "de") {
		return "de"
	}
	return "en"
}

type officePage struct {
	Title       string
	Subpath     string
	ParentURL   string
	DownloadURL string
	APIScript   string
	Config      template.JS
}

var (
	officeTemplateOnce sync.Once
	officeTemplate     *template.Template
	officeTemplateErr  error
)

func loadOfficeTemplate() (*template.Template, error) {
	officeTemplateOnce.Do(func() {
		officeTemplate, officeTemplateErr = template.New("office").ParseFiles(officeHtmlPath)
	})
	return officeTemplate, officeTemplateErr
}

// serveOfficeViewer renders the page that embeds the document server editor.
func serveOfficeViewer(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	tmpl, err := loadOfficeTemplate()
	if err != nil {
		GoLog.Errorf("failed to load office template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	downloadPath := r.URL.Path + "?dl=1"
	name := ctx.fileInfo.Name()

	cfg := officeConfig(officeDoc{
		name: name,
		key:  officeDocKey(ctx.diskPath, ctx.fileInfo),
		url:  scheme + "://" + r.Host + downloadPath,
	}, requestLang(r), ctx.config.OfficeSecret)
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	parent := filepath.Dir(r.URL.Path)
	if parent == "/"+ctx.subpath || parent == "." {
		parent = "/" + ctx.subpath
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "office", officePage{
		Title:       name,
		Subpath:     ctx.subpath,
		ParentURL:   parent,
		DownloadURL: downloadPath,
		APIScript:   ctx.config.OfficeURL + "/web-apps/apps/api/documents/api.js",
		Config:      template.JS(cfgJSON),
	}); err != nil {
		GoLog.Errorf("failed to render office template: %v", err)
	}
}
