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
	"github.com/Wirezat/fileshare/internal/web"
)

const (
	configPath    = "./data.json"
	chunkTempBase = "/tmp/fileshare-chunks"
)

func chunkTimeout(st *store.FileStore) func() time.Duration {
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

	srv, err := web.New(web.Deps{
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
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}
