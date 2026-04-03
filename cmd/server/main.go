package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

func buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	adminRoutes := map[string]http.HandlerFunc{
		"/admin":                            handleAdminUI,
		"/admin/static/admin.css":           handleAdminCSS,
		"/admin/static/admin.js":            handleAdminJS,
		"/admin/api/shares":                 handleAdminShares,
		"/admin/api/logs":                   handleAdminLogs,
		"/admin/api/logs/stream":            handleAdminLogsStream,
		"/admin/api/settings/password":      handleAdminSettingsPassword,
		"/admin/api/settings/prune_expired": handleAdminFunctionPruneExpired,
	}
	for path, h := range adminRoutes {
		mux.HandleFunc(path, basicAuth(h))
	}

	// Public routes
	mux.HandleFunc("/", handleRequest)
	mux.HandleFunc("/static/share.css", handleShareCSS)
	mux.HandleFunc("/static/share.js", handleShareJS)

	return mux
}

func startServer(port int) {
	addr := fmt.Sprintf(":%d", port)
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

	// load log history and start tailing
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

	startExpirationWatcher(5 * time.Minute)
	startServer(config.Port)
}
