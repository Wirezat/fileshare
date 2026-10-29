package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/upload"
)

const timeout = 5 * time.Second

// Notifier posts a short message to an ntfy topic for every finished upload.
type Notifier struct {
	view   func() config.View
	client *http.Client
}

// New returns a Notifier that reads the ntfy server, topic, access token and the shares from view on every upload; an empty server or topic turns it off.
func New(view func() config.View) *Notifier {
	return &Notifier{view: view, client: &http.Client{Timeout: timeout}}
}

// Published sends the message for p; it returns when the request is done.
func (n *Notifier) Published(p upload.Published) {
	cfg := n.view()
	if cfg.WebhookServer() == "" || cfg.WebhookTopic() == "" {
		return
	}
	target := cfg.WebhookServer() + "/" + cfg.WebhookTopic()
	share, rel := locate(cfg, p.DestDir)
	body := fmt.Sprintf("%s (%s)", p.Filename, size(p.Size))
	if rel != "" {
		body += "\n" + rel
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		GoLog.Warnf("upload webhook: %v", err)
		return
	}
	req.Header.Set("Title", "Upload in "+share)
	if token := cfg.WebhookToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		GoLog.Warnf("upload webhook: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		GoLog.Warnf("upload webhook: server answered %s", resp.Status)
	}
}

// locate returns the subpath of the share whose folder holds dir, and dir relative to that folder ("" for the folder itself).
func locate(cfg config.View, dir string) (share, rel string) {
	dir = filepath.Clean(dir)
	best := -1
	for sub, fd := range cfg.Shares() {
		root := filepath.Clean(fd.Path)
		r, err := filepath.Rel(root, dir)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			continue
		}
		if len(root) > best {
			best, share, rel = len(root), sub, r
		}
	}
	if best < 0 {
		return "?", dir
	}
	if rel == "." {
		rel = ""
	}
	return share, rel
}

func size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f, suffix := float64(n), ""
	for _, s := range []string{"KB", "MB", "GB", "TB"} {
		f /= unit
		suffix = s
		if f < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", f, suffix)
}
