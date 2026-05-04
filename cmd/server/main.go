package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

var startTime = time.Now()
var storage *LocalStorage

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
		"/admin":                                       handleAdminUI,
		"/admin/static/admin.css":                      handleAdminCSS,
		"/admin/static/admin.js":                       handleAdminJS,
		"/admin/api/shares":                            handleAdminShares,
		"/admin/api/logs":                              handleAdminLogs,
		"/admin/api/logs/stream":                       handleAdminLogsStream,
		"/admin/api/settings/username":                 handleAdminSettingsUsername,
		"/admin/api/settings/password":                 handleAdminSettingsPassword,
		"/admin/api/settings/max_post_size":            handleAdminSettingsMaxPostSize,
		"/admin/api/settings/chunk_inactivity_timeout": handleAdminSettingsChunkInactivityTimeout,
		"/admin/api/settings/prune_expired":            handleAdminFunctionPruneExpired,
		"/admin/api/runtime":                           handleAdminRuntime,
	}
	for path, h := range adminRoutes {
		mux.HandleFunc(path, basicAuth(h))
	}

	// Setup routes — no auth, and no logging to avoid capturing password setup attempts.
	mux.HandleFunc("GET /setup", handleSetupUI)
	mux.HandleFunc("POST /setup/api/init", handleSetupInit)

	// Chunk upload endpoints — handlers parse multipart themselves.
	mux.HandleFunc("POST /{subpath}/chunk-init", handleChunkInit)
	mux.HandleFunc("POST /{subpath}/chunk", handleChunkReceive)

	// Share unlock — not wrapped in loggingMiddleware (form body contains password).
	mux.HandleFunc("POST /{subpath}/unlock", handleUnlock)

	// Public routes.
	public := chain(
		http.HandlerFunc(handleRequest),
		loggingMiddleware,
	)
	mux.Handle("/", public)
	mux.HandleFunc("/static/share.css", handleShareCSS)
	mux.HandleFunc("/static/share.js", handleShareJS)

	return mux
}

func startServer(config *shared.Config) {
	addr := fmt.Sprintf(":%d", config.Port)
	GoLog.Infof("Server running at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, buildMux()); err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}

func main() {
	if err := GoLog.ToFile(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	if path := GoLog.LogPath(); path != "" {
		if err := shared.Logger.Load(path); err != nil {
			GoLog.Errorf("failed to load log history: %v", err)
		}
		if err := shared.Logger.Tail(path); err != nil {
			GoLog.Errorf("failed to start log tail: %v", err)
		}
	}

	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}

	storage = NewLocalStorage(config)
	storage.StartReaper()
	startExpirationWatcher(5 * time.Minute)
	startServer(config)
}
