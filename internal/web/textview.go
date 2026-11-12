package web

import (
	"bytes"
	"html"
	"io"
	"net/http"
	"os"

	"github.com/Wirezat/GoLog"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/Wirezat/fileshare/internal/filetype"
)

const (
	textViewMaxBytes = 1 << 20
	sniffBytes       = 1024
)

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// readCapped returns the first MiB of the requested file, cut before a split character;
// truncated reports whether the file is longer.
func readCapped(ctx *requestContext) (data []byte, truncated bool, err error) {
	f, err := os.Open(ctx.diskPath)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, textViewMaxBytes))
	if err != nil {
		return nil, false, err
	}
	return filetype.TrimIncompleteRune(data), ctx.fileInfo.Size() > textViewMaxBytes, nil
}

// sniffsAsText reports whether the first KiB of the requested file looks like text.
func sniffsAsText(ctx *requestContext) bool {
	f, err := os.Open(ctx.diskPath)
	if err != nil {
		return false
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, sniffBytes))
	return err == nil && filetype.Sniff(head)
}

// writeFragment answers with an HTML fragment built from the first MiB of the file and flags a cut-off file.
func writeFragment(w http.ResponseWriter, r *http.Request, ctx *requestContext, render func([]byte) ([]byte, error)) {
	if notModified(w, r, ctx.fileInfo) {
		return
	}
	raw, truncated, err := readCapped(ctx)
	var out []byte
	if err == nil {
		out, err = render(raw)
	}
	if err != nil {
		GoLog.Errorf("text view %s: %v", ctx.diskPath, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if truncated {
		w.Header().Set("X-Fileshare-Truncated", "1")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(out)
}

// escapedPre returns raw HTML-escaped inside <pre>; it never fails.
func escapedPre(raw []byte) ([]byte, error) {
	return []byte("<pre>" + html.EscapeString(string(raw)) + "</pre>"), nil
}

// renderedMarkdown returns raw rendered as markdown inside <div class="markdown">, or the render error.
func renderedMarkdown(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<div class="markdown">`)
	if err := markdown.Convert(raw, &buf); err != nil {
		return nil, err
	}
	buf.WriteString(`</div>`)
	return buf.Bytes(), nil
}

// serveTextView answers ?view=text with an HTML fragment: rendered markdown for .md, the escaped file inside
// <pre> for every other text file and for unknown files that sniff as text; unknown binary files get 415.
func serveTextView(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	view := fileView(ctx)
	switch {
	case view == filetype.ViewProbe && !sniffsAsText(ctx):
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
	case view == filetype.ViewMarkdown:
		writeFragment(w, r, ctx, renderedMarkdown)
	case view == filetype.ViewProbe || filetype.Lookup(ctx.fileInfo.Name()).Kind == filetype.Text:
		writeFragment(w, r, ctx, escapedPre)
	default:
		http.NotFound(w, r)
	}
}

// serveSourceView answers ?view=source with the escaped markdown or HTML source inside <pre>.
func serveSourceView(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if v := fileView(ctx); v != filetype.ViewMarkdown && v != filetype.ViewHTML {
		http.NotFound(w, r)
		return
	}
	writeFragment(w, r, ctx, escapedPre)
}
