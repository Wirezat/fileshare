package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/access"
	"github.com/Wirezat/fileshare/internal/auth"
	"github.com/Wirezat/fileshare/internal/logstore"
	"github.com/Wirezat/fileshare/internal/settings"
	"github.com/Wirezat/fileshare/internal/share"
	"github.com/Wirezat/fileshare/internal/stats"
	"github.com/Wirezat/fileshare/internal/store"
	"github.com/Wirezat/fileshare/internal/throttle"
	"github.com/Wirezat/fileshare/internal/upload"
	"github.com/Wirezat/fileshare/internal/web"
)

const (
	shutdownTimeout = 10 * time.Second
	healthTimeout   = 3 * time.Second
	logRetention    = 30 * 24 * time.Hour
)

func paths(s settings.Settings) (config, stats, chunks string) {
	return filepath.Join(s.DataDir, "data.json"), filepath.Join(s.DataDir, "stats.json"), filepath.Join(s.DataDir, "chunks")
}

const initialConfig = `{
  "port": 27182,
  "maxPostSize": 107374182400,
  "chunkInactivityTimeout": 3600,
  "admin_username": "",
  "admin_password": "",
  "files": {}
}
`

// ensureConfig creates the parent directory and writes the initial config at path when no file exists there; an existing file is left alone.
func ensureConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(initialConfig)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func listenAddr(s settings.Settings, port int) string {
	return net.JoinHostPort(s.Bind, strconv.Itoa(port))
}

func healthAddr(s settings.Settings, port int) string {
	host := s.Bind
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func chunkTimeout(st *store.FileStore) func() time.Duration {
	return func() time.Duration {
		return time.Duration(st.Current().ChunkInactivityTimeout()) * time.Second
	}
}

// runCheck writes one "<subpath>\t<problem>" line per broken share, sorted by subpath, without creating or changing any file.
// It returns an error when the settings or data.json cannot be read.
func runCheck(w io.Writer, getenv func(string) string) error {
	cfg, err := settings.Load(getenv)
	if err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}
	configPath, _, _ := paths(cfg)
	st, err := store.Open(configPath)
	if err != nil {
		return err
	}
	problems := share.New(st).Problems()
	for _, subpath := range slices.Sorted(maps.Keys(problems)) {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", subpath, problems[subpath]); err != nil {
			return err
		}
	}
	return nil
}

// runHealthcheck requests /healthz on the address the server listens on and returns an error unless it answers 200.
// The port comes from FS_PORT, else from data.json when readable, else the default; nothing is created or logged.
func runHealthcheck(getenv func(string) string, client *http.Client) error {
	cfg, err := settings.Load(getenv)
	if err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}
	fromConfig := 0
	if cfg.Port == 0 {
		configPath, _, _ := paths(cfg)
		if st, err := store.Open(configPath); err == nil {
			fromConfig = st.Current().Port()
		}
	}
	resp, err := client.Get("http://" + healthAddr(cfg, cfg.ResolvePort(fromConfig)) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %s", resp.Status)
	}
	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(os.Getenv, &http.Client{Timeout: healthTimeout}); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "check" {
		if err := runCheck(os.Stdout, os.Getenv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	cfg, err := settings.Load(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid settings: %v\n", err)
		os.Exit(1)
	}
	GoLog.SetPrivacy(true)
	logs := logstore.New()
	GoLog.OnRecord = func(r GoLog.Record) {
		logs.Add(logstore.Entry{Level: r.Level, Time: r.Time, Message: r.Message})
	}

	initLog := GoLog.ToFile
	if cfg.LogDir != "" {
		initLog = func() error { return GoLog.ToDir(cfg.LogDir) }
	}
	if err := initLog(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if err := GoLog.SetRetention(logRetention); err != nil {
		GoLog.Errorf("failed to delete old logs: %v", err)
	}

	if path := GoLog.LogPath(); path != "" {
		if err := logs.Load(path); err != nil {
			GoLog.Errorf("failed to load log history: %v", err)
		}
	}

	configPath, statsPath, chunkBase := paths(cfg)
	if err := ensureConfig(configPath); err != nil {
		GoLog.Errorf("failed to create initial config: %v", err)
		os.Exit(1)
	}
	accessBroken := false
	if err := access.SelfTest(cfg.DataDir); err != nil {
		GoLog.Errorf("permission checks unavailable (%v), every folder is shown as locked", err)
		accessBroken = true
	}
	exe, err := os.Executable()
	if err != nil {
		GoLog.Errorf("failed to locate executable: %v", err)
		os.Exit(1)
	}

	st, err := store.Open(configPath)
	if err != nil {
		GoLog.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}
	counts := stats.Open(statsPath)
	srv, admins, err := build(st, counts, logs, GoLog.Writer(), filepath.Join(filepath.Dir(exe), "web"), chunkBase, accessBroken)
	if err != nil {
		GoLog.Errorf("failed to load web templates: %v", err)
		os.Exit(1)
	}

	if code := admins.SetupCode(); code != "" {
		GoLog.Infof("no admin password set — open /setup and enter this setup code: %s", code)
	} else if admins.NeedsSetup() {
		GoLog.Errorf("setup: failed to generate setup code")
	}

	addr := listenAddr(cfg, cfg.ResolvePort(st.Current().Port()))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)
	GoLog.Infof("Server running at http://%s", addr)
	err = serve(ctx, httpServer(srv), ln, shutdownTimeout)
	if cerr := counts.Close(); cerr != nil {
		GoLog.Errorf("failed to save stats: %v", cerr)
	}
	if err != nil {
		GoLog.Errorf("server stopped unexpectedly: %v", err)
		os.Exit(1)
	}
}

func httpServer(srv *web.Server) *http.Server {
	hs := &http.Server{Handler: srv.Handler()}
	hs.RegisterOnShutdown(srv.CloseStreams)
	return hs
}

func build(st *store.FileStore, counts *stats.Stats, logs *logstore.Store, requestLog io.Writer, assets, chunkBase string, accessBroken bool) (*web.Server, *auth.Service, error) {
	admins := auth.New(st)
	shares := share.New(st)
	problems := shares.Problems()
	for _, subpath := range slices.Sorted(maps.Keys(problems)) {
		GoLog.Warnf("share /%s: %s", subpath, problems[subpath])
	}
	uploads := upload.New(chunkBase, chunkTimeout(st))
	uploads.Observe(counts)
	uploads.Sweep()
	srv, err := web.New(web.Deps{
		Store:      st,
		Shares:     shares,
		Auth:       admins,
		Uploads:    uploads,
		Logs:       logs,
		Throttle:   throttle.New(func() (bool, int) { return st.Current().DownloadLimit() }),
		Stats:      counts,
		RequestLog: requestLog,
		Assets:     assets,
		Started:    time.Now(),

		AccessBroken: accessBroken,
	})
	return srv, admins, err
}

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
