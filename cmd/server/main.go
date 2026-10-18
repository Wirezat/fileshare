package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	configPath      = "./data.json"
	chunkTempBase   = "/tmp/fileshare-chunks"
	shutdownTimeout = 10 * time.Second
)

func chunkTimeout(st *store.FileStore) func() time.Duration {
	return func() time.Duration {
		return time.Duration(st.Current().ChunkInactivityTimeout()) * time.Second
	}
}

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
	srv, admins, err := build(st, logs, requestLog, "./web", chunkTempBase)
	if err != nil {
		GoLog.Errorf("failed to load web templates: %v", err)
		os.Exit(1)
	}

	if code := admins.SetupCode(); code != "" {
		GoLog.Infof("no admin password set — open /setup and enter this setup code: %s", code)
	} else if admins.NeedsSetup() {
		GoLog.Errorf("setup: failed to generate setup code")
	}

	addr := fmt.Sprintf(":%d", st.Current().Port())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	GoLog.Infof("Server running at http://localhost%s", addr)
	if err := serve(ctx, &http.Server{Handler: srv.Handler()}, ln, shutdownTimeout); err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}

func build(st *store.FileStore, logs *logstore.Store, requestLog io.Writer, assets, chunkBase string) (*web.Server, *auth.Service, error) {
	admins := auth.New(st)
	uploads := upload.New(chunkBase, chunkTimeout(st))
	uploads.Sweep()
	srv, err := web.New(web.Deps{
		Store:      st,
		Shares:     share.New(st),
		Auth:       admins,
		Uploads:    uploads,
		Logs:       logs,
		RequestLog: requestLog,
		Assets:     assets,
		Started:    time.Now(),
	})
	return srv, admins, err
}

// serve runs hs on ln until ctx ends, then gives open requests timeout to finish before closing them; it returns any other serve error.
func serve(ctx context.Context, hs *http.Server, ln net.Listener, timeout time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	GoLog.Infof("shutting down, open requests get %s", timeout)
	sctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		GoLog.Warnf("shutdown: %v, closing open connections", err)
		hs.Close()
	}
	return nil
}
