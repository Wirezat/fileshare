package main

import (
	"fmt"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

func startServer(port int) {
	http.HandleFunc("/admin", basicAuth(handleAdminUI))
	http.HandleFunc("/admin/api/shares", basicAuth(handleAdminShares))
	http.HandleFunc("/", handleRequest)
	GoLog.Infof(fmt.Sprintf("Server running at http://localhost:%d", port))
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

	config, err := shared.LoadConfig()
	if err != nil {
		GoLog.Errorf("error loading config: %v", err)
		return
	}

	startServer(config.Port)
}
