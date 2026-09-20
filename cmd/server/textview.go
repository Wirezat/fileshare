package main

import (
	"bytes"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const textViewMaxBytes = 1 << 20

var textExt = map[string]bool{
	"txt": true, "md": true, "log": true, "csv": true, "json": true, "xml": true,
	"yaml": true, "yml": true, "toml": true, "ini": true, "cfg": true, "conf": true,
	"sh": true, "py": true, "go": true, "js": true, "ts": true, "css": true, "html": true,
	"java": true, "c": true, "h": true, "cpp": true, "rs": true, "sql": true,
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// isText reports whether name has an extension the listing previews as text.
func isText(name string) bool {
	return textExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]
}

func textWanted(r *http.Request, ctx *requestContext) bool {
	return !ctx.fileInfo.IsDir() && r.URL.Query().Get("view") == "text"
}

// serveTextView answers ?view=text with an HTML fragment: rendered markdown
// for .md, the escaped file inside <pre> for every other text extension.
func serveTextView(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	name := ctx.fileInfo.Name()
	if !isText(name) {
		http.NotFound(w, r)
		return
	}
	if ctx.fileInfo.Size() > textViewMaxBytes {
		http.Error(w, "File too large to preview", http.StatusRequestEntityTooLarge)
		return
	}
	raw, err := os.ReadFile(ctx.diskPath)
	if err != nil {
		GoLog.Errorf("text view %s: %v", ctx.diskPath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.EqualFold(filepath.Ext(name), ".md") {
		var buf bytes.Buffer
		if err := markdown.Convert(raw, &buf); err != nil {
			GoLog.Errorf("markdown %s: %v", ctx.diskPath, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`<div class="markdown">`))
		w.Write(buf.Bytes())
		w.Write([]byte(`</div>`))
		return
	}
	w.Write([]byte("<pre>"))
	w.Write([]byte(html.EscapeString(string(raw))))
	w.Write([]byte("</pre>"))
}
