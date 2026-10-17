package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/share"
	"github.com/Wirezat/fileshare/internal/store"
	"github.com/Wirezat/fileshare/internal/upload"
)

const (
	configPath    = "./data.json"
	chunkTempBase = "/tmp/fileshare-chunks"
)

// chain applies middleware in order: first wraps outermost, last wraps innermost.
func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func (s *server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	adminRoutes := map[string]http.HandlerFunc{
		"/admin/api/shares":                            s.handleAdminShares,
		"/admin/api/shares/qr":                         s.handleAdminShareQR,
		"/admin/api/browse":                            handleAdminBrowse,
		"/admin/api/logs":                              s.handleAdminLogs,
		"/admin/api/logs/stream":                       s.handleAdminLogsStream,
		"/admin/api/settings/username":                 s.handleAdminSettingsUsername,
		"/admin/api/settings/password":                 s.handleAdminSettingsPassword,
		"/admin/api/settings/max_post_size":            s.handleAdminSettingsMaxPostSize,
		"/admin/api/settings/chunk_inactivity_timeout": s.handleAdminSettingsChunkInactivityTimeout,
		"/admin/api/settings/office":                   s.handleAdminSettingsOffice,
		"/admin/api/settings/prune_expired":            s.handleAdminFunctionPruneExpired,
		"/admin/api/uptime":                            s.handleAdminUptime,

		"/admin":          s.handleAdminUI,
		"/admin/logs":     s.handleAdminLogsPage,
		"/admin/settings": s.handleAdminSettingsPage,
		"/admin/api/me":   s.handleAdminMe,
	}
	for path, h := range adminRoutes {
		mux.HandleFunc(path, s.adminAuth(h))
	}

	uiFS := http.StripPrefix("/static/ui/", http.FileServer(http.Dir(s.asset("ui"))))
	mux.Handle("/static/ui/", uiFS)
	mux.HandleFunc("/static/theme.css", s.handleThemeCSS)

	jsFS := http.StripPrefix("/admin/static/js/", http.FileServer(http.Dir(s.asset("js/admin"))))
	mux.HandleFunc("/admin/static/js/", s.adminAuth(jsFS.ServeHTTP))

	mux.HandleFunc("/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/admin/logout", s.handleAdminLogout)

	mux.HandleFunc("GET /setup", s.handleSetupUI)
	mux.HandleFunc("POST /setup/api/init", s.handleSetupInit)

	mux.HandleFunc("POST /{subpath}/chunk-init", s.handleChunkInit)
	mux.HandleFunc("POST /{subpath}/chunk", s.handleChunkReceive)

	mux.HandleFunc("POST /{subpath}/unlock", s.handleUnlock)

	public := chain(
		http.HandlerFunc(s.handleRequest),
		s.loggingMiddleware,
	)
	mux.Handle("/", public)
	mux.HandleFunc("/static/share.css", s.handleShareCSS)
	mux.HandleFunc("/static/share.js", s.handleShareJS)

	mux.Handle("/static/locales/", http.StripPrefix("/static/locales/", http.FileServer(http.Dir(s.asset("locales")))))

	return mux
}

// chunkTimeout returns the current chunk inactivity timeout of st.
func chunkTimeout(st configStore) func() time.Duration {
	return func() time.Duration {
		return time.Duration(st.Current().ChunkInactivityTimeout()) * time.Second
	}
}

// openRequestLog opens GoLog's log file for appending request-log entries, or returns nil.
func openRequestLog() io.Writer {
	path := GoLog.LogPath()
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		GoLog.Warnf("openRequestLog: %v", err)
		return nil
	}
	return f
}

func main() {
	logs := logstore.New()
	GoLog.OnRecord = func(r GoLog.Record) {
		logs.Add(logstore.Entry{Level: r.Level, Time: r.Time, Message: r.Message})
	}

	if err := GoLog.ToFile(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	requestLog := openRequestLog()

	if path := GoLog.LogPath(); path != "" {
		if err := logs.Load(path); err != nil {
			GoLog.Errorf("failed to load log history: %v", err)
		}
	}

	st, err := store.Open(configPath)
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}
	admins := auth.New(st)
	if code := admins.SetupCode(); code != "" {
		GoLog.Infof("no admin password set — open /setup and enter this setup code: %s", code)
	} else if admins.NeedsSetup() {
		GoLog.Errorf("setup: failed to generate setup code")
	}

	uploads := upload.New(chunkTempBase, chunkTimeout(st))
	uploads.Sweep()

	srv, err := newHTTPServer(deps{
		Store:      st,
		Shares:     share.New(st),
		Auth:       admins,
		Uploads:    uploads,
		Logs:       logs,
		RequestLog: requestLog,
		Assets:     "./web",
		Started:    time.Now(),
	})
	if err != nil {
		GoLog.Errorf("failed to load web templates: %v", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf(":%d", st.Current().Port())
	GoLog.Infof("Server running at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, srv.handler()); err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}
