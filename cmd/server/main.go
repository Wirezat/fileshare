package main

import (
	"fmt"
	"net/http"
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
	err := GoLog.ToFile()
	if err != nil {
		GoLog.Errorf("error initializing logger: %v", err)
		return
	}

	logPath := GoLog.LogPath()
	if logPath != "" {
		GoLog.Infof("Loading existing logs from %s", logPath)
		if err := shared.Logger.LoadFromFile(logPath); err != nil {
			GoLog.Errorf("error loading logs: %v", err)
		}
	}

	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("error loading config: %v", err)
		return
	}

	startExpirationWatcher(5 * time.Minute)
	startServer(config.Port)
}
