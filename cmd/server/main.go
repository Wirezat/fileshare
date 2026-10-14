package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/store"
	"github.com/Wirezat/fileshare/internal/upload"
	"github.com/Wirezat/fileshare/pkg/shared"
)

const (
	configPath    = "./data.json"
	chunkTempBase = "/tmp/fileshare-chunks"
)

var startTime = time.Now()
var (
	storage  *upload.Storage
	cfgStore *store.FileStore
)

// chain applies middleware in order: first wraps outermost, last wraps innermost.
func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	adminRoutes := map[string]http.HandlerFunc{
		"/admin/api/shares":                            handleAdminShares,
		"/admin/api/shares/qr":                         handleAdminShareQR,
		"/admin/api/browse":                            handleAdminBrowse,
		"/admin/api/logs":                              handleAdminLogs,
		"/admin/api/logs/stream":                       handleAdminLogsStream,
		"/admin/api/settings/username":                 handleAdminSettingsUsername,
		"/admin/api/settings/password":                 handleAdminSettingsPassword,
		"/admin/api/settings/max_post_size":            handleAdminSettingsMaxPostSize,
		"/admin/api/settings/chunk_inactivity_timeout": handleAdminSettingsChunkInactivityTimeout,
		"/admin/api/settings/office":                   handleAdminSettingsOffice,
		"/admin/api/settings/prune_expired":            handleAdminFunctionPruneExpired,
		"/admin/api/uptime":                            handleAdminUptime,

		"/admin":          handleAdminUI,
		"/admin/logs":     handleAdminLogsPage,
		"/admin/settings": handleAdminSettingsPage,
		"/admin/api/me":   handleAdminMe,
	}
	for path, h := range adminRoutes {
		mux.HandleFunc(path, adminAuth(h))
	}

	uiFS := http.StripPrefix("/static/ui/", http.FileServer(http.Dir(uiDir)))
	mux.Handle("/static/ui/", uiFS)
	mux.HandleFunc("/static/theme.css", handleThemeCSS)

	jsFS := http.StripPrefix("/admin/static/js/", http.FileServer(http.Dir(adminJsDir)))
	mux.HandleFunc("/admin/static/js/", adminAuth(jsFS.ServeHTTP))

	// Admin login/logout — no auth middleware.
	mux.HandleFunc("/admin/login", handleAdminLogin)
	mux.HandleFunc("/admin/logout", handleAdminLogout)

	mux.HandleFunc("GET /setup", handleSetupUI)
	mux.HandleFunc("POST /setup/api/init", handleSetupInit)

	mux.HandleFunc("POST /{subpath}/chunk-init", handleChunkInit)
	mux.HandleFunc("POST /{subpath}/chunk", handleChunkReceive)

	mux.HandleFunc("POST /{subpath}/unlock", handleUnlock)

	// Public routes.
	public := chain(
		http.HandlerFunc(handleRequest),
		loggingMiddleware,
	)
	mux.Handle("/", public)
	mux.HandleFunc("/static/share.css", handleShareCSS)
	mux.HandleFunc("/static/share.js", handleShareJS)

	mux.Handle("/static/locales/", http.StripPrefix("/static/locales/", http.FileServer(http.Dir(localesDir))))

	return mux
}

func startServer(port int) {
	addr := fmt.Sprintf(":%d", port)
	GoLog.Infof("Server running at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, securityHeaders(buildMux())); err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}

func chunkTimeout() time.Duration {
	return time.Duration(cfgStore.Current().ChunkInactivityTimeout()) * time.Second
}

func main() {
	GoLog.OnRecord = func(r GoLog.Record) {
		shared.Logger.Add(shared.LogEntry{Level: r.Level, Time: r.Time, Message: r.Message})
	}

	if err := GoLog.ToFile(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	openRequestLog()

	if path := GoLog.LogPath(); path != "" {
		if err := shared.Logger.Load(path); err != nil {
			GoLog.Errorf("failed to load log history: %v", err)
		}
	}

	var err error
	cfgStore, err = store.Open(configPath)
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}

	initSetupToken(cfgStore.Current())

	storage = upload.New(chunkTempBase, chunkTimeout)
	storage.Sweep()
	startServer(cfgStore.Current().Port())
}
