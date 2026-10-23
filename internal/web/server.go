package web

import (
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/media"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/share"
)

type configStore interface {
	Current() config.View
	Update(func(*config.Config) error) error
	Refused() string
}

type shareService interface {
	Resolve(subpath, rel string, a share.Access) (share.Resolved, error)
	Unlock(subpath, pw, clientKey string) (string, error)
	UploadTarget(subpath, token string) (config.FileData, error)
	Create(subpath string, fd config.FileData) error
	Patch(subpath string, p share.Patch) ([]string, error)
	Delete(subpath string) (config.FileData, error)
	Prune() (int, error)
	Problems() map[string]string
}

type authService interface {
	NeedsSetup() bool
	Login(user, pass, clientKey string) (string, error)
	Logout(token string)
	Valid(token string) bool
	ChangeUsername(current, name, keep string) (int, error)
	ChangePassword(current, next, keep string) (int, error)
	Setup(code, user, pass, clientKey string) error
}

type uploadService interface {
	Init(uploadID, filename string, size, chunkSize int64, destDir string) ([]int, error)
	Receive(uploadID, destDir string, index int, r io.Reader) (bool, error)
}

type throttler interface {
	Handler(next http.Handler, client func(*http.Request) string) http.Handler
}

type logService interface {
	Add(entry logstore.Entry)
	Recent(n int) []logstore.Entry
	Subscribe() chan logstore.Entry
	Unsubscribe(ch chan logstore.Entry)
}

// Deps are the services and settings a server is built from; RequestLog and Stats may be nil.
type Deps struct {
	Store      configStore
	Shares     shareService
	Auth       authService
	Uploads    uploadService
	Logs       logService
	Throttle   throttler
	Stats      statsService
	RequestLog io.Writer
	Assets     string
	Started    time.Time

	AccessBroken bool
}

type templates struct {
	directory, gate, notFound, expired, office, preview *template.Template
}

type Server struct {
	deps         Deps
	tpl          templates
	requestLogMu sync.Mutex
	streamsDone  chan struct{}
	closeStreams sync.Once
}

// New parses all page templates under d.Assets and returns a server, or the first parse error.
func New(d Deps) (*Server, error) {
	html := func(name string) string { return filepath.Join(d.Assets, "html", name) }
	var t templates
	var err error
	parse := func(dst **template.Template, tmpl *template.Template, file string) {
		if err == nil {
			*dst, err = tmpl.ParseFiles(html(file))
		}
	}
	parse(&t.directory, template.New("directory").Funcs(template.FuncMap{"opensInline": office.OpensInline, "isText": media.IsText}), "share.html")
	parse(&t.gate, template.New("gate"), "gate.html")
	parse(&t.notFound, template.New("notfound"), "notfound.html")
	parse(&t.expired, template.New("expired"), "expired.html")
	parse(&t.office, template.New("office"), "office.html")
	parse(&t.preview, template.New("preview"), "preview.html")
	if err != nil {
		return nil, err
	}
	return &Server{deps: d, tpl: t, streamsDone: make(chan struct{})}, nil
}

func (s *Server) CloseStreams() {
	s.closeStreams.Do(func() { close(s.streamsDone) })
}

// asset returns the path of rel inside the assets directory.
func (s *Server) asset(rel string) string {
	return filepath.Join(s.deps.Assets, rel)
}

// Handler returns all routes behind the security headers.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.buildMux())
}
