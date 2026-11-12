package web

import (
	"net/http"

	"github.com/Wirezat/fileshare/internal/filetype"
)

// allowSameOriginFraming lets pages of this site embed the response; csp replaces the Content-Security-Policy.
func allowSameOriginFraming(w http.ResponseWriter, csp string) {
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
}

// serveFrame answers ?view=frame with a PDF or HTML file that the share page may embed; HTML stays sandboxed.
func serveFrame(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	switch fileView(ctx) {
	case filetype.ViewPDF:
		allowSameOriginFraming(w, "frame-ancestors 'self'")
	case filetype.ViewHTML:
		allowSameOriginFraming(w, "sandbox; frame-ancestors 'self'")
	default:
		http.NotFound(w, r)
		return
	}
	if notModified(w, r, ctx.fileInfo) {
		return
	}
	http.ServeFile(w, r, ctx.diskPath)
}
