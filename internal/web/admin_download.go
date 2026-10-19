package web

import (
	"errors"
	"net/http"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/config"
)

// handleAdminSettingsDownloadLimit reads and sets the download limit.
//
//	GET   /admin/api/settings/download_limit → {"enabled":true,"mbit":50}
//	PATCH /admin/api/settings/download_limit   Body: {"enabled":true,"mbit":50}; an omitted mbit keeps the stored rate
func (s *Server) handleAdminSettingsDownloadLimit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		on, mbit := s.deps.Store.Current().DownloadLimit()
		jsonResponse(w, map[string]any{"enabled": on, "mbit": mbit})

	case http.MethodPatch:
		var req struct {
			Enabled *bool `json:"enabled"`
			Mbit    *int  `json:"mbit"`
		}
		if !decodeOrErr(w, r, &req) {
			return
		}
		if req.Enabled == nil {
			http.Error(w, "enabled is required", http.StatusBadRequest)
			return
		}
		err := s.deps.Store.Update(func(c *config.Config) error {
			return c.SetDownloadLimit(*req.Enabled, req.Mbit)
		})
		if errors.Is(err, config.ErrInvalidDownloadLimit) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			GoLog.Errorf("failed to update config: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		on, mbit := s.deps.Store.Current().DownloadLimit()
		GoLog.Infof("download limit set: enabled=%v, %d Mbit/s", on, mbit)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
