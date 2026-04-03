package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

func startServer(port int) {
	http.HandleFunc("/admin", basicAuth(handleAdminUI))
	http.HandleFunc("/admin/static/admin.css", basicAuth(handleAdminCSS))
	http.HandleFunc("/admin/static/admin.js", basicAuth(handleAdminJS))
	http.HandleFunc("/admin/api/shares", basicAuth(handleAdminShares))
	http.HandleFunc("/admin/api/logs", basicAuth(handleAdminLogs))
	http.HandleFunc("/admin/api/logs/stream", basicAuth(handleAdminLogsStream))
	http.HandleFunc("/admin/api/settings/password", basicAuth(handleAdminSettingsPassword))
	http.HandleFunc("/admin/api/settings/prune_expired", basicAuth(handleAdminFunctionPruneExpired))
	http.HandleFunc("/", handleRequest)
	http.HandleFunc("/static/share.css", handleShareCSS)
	http.HandleFunc("/static/share.js", handleShareJS)
	GoLog.Infof("Server running at http://localhost:%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		GoLog.Errorf("failed to start server: %v", err)
	}
}

func main() {
	if err := GoLog.ToFile(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	if path := GoLog.LogPath(); path != "" {
		if err := shared.Logger.Load(path); err != nil {
			GoLog.Errorf("failed to load logs: %v", err)
		}
		if err := shared.Logger.Tail(path); err != nil {
			GoLog.Errorf("failed to tail logs: %v", err)
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
