package web

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/archive"
)

// zipAndServe sends a ZIP of dirPath, limited to roots when given, as a download.
func zipAndServe(w http.ResponseWriter, dirPath string, roots []string) {
	folderName := filepath.Base(filepath.Clean(dirPath))
	zipName := folderName
	if len(roots) > 0 {
		zipName += "-selection"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, zipName))
	if err := archive.Write(w, dirPath, roots); err != nil {
		GoLog.Errorf("zip %s: %v", folderName, err)
		return
	}
	GoLog.Infof("zip served: %s", folderName)
}
