package web

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/access"
	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/filetype"
	"github.com/Wirezat/fileshare/internal/media"
	"github.com/Wirezat/fileshare/internal/office"
	"github.com/Wirezat/fileshare/internal/stats"
	"github.com/Wirezat/fileshare/internal/upload"
)

// serveDirectory renders the directory listing, or streams a ZIP if ?download=zip.
// Repeated f= parameters restrict the ZIP to those entries of the directory.
func (s *Server) serveDirectory(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if r.URL.Query().Get("download") == "zip" {
		if ctx.fileData.NoZip {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		roots, ok := zipSelection(r.URL.Query()["f"])
		if !ok {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodGet {
			zipAndServe(w, ctx.diskPath, roots)
			return
		}
		dw := s.countDownload(w, stats.Download{Share: ctx.subpath, Zip: true, UserAgent: r.UserAgent()})
		served := false
		defer func() { dw.finish(served) }()
		zipAndServe(dw, ctx.diskPath, roots)
		served = true
		return
	}
	if r.Method == http.MethodGet && isPageLoad(r) {
		s.stats().PageView(r.UserAgent())
	}

	tmpl := s.tpl.directory

	fd := ctx.fileData
	relPath := filepath.Join("/", strings.TrimPrefix(ctx.diskPath, fd.Path))

	parentDir := "/"
	if ctx.diskPath != fd.Path {
		parentDir = filepath.Join("/", strings.TrimPrefix(filepath.Dir(ctx.diskPath), fd.Path))
	}

	dirName := ctx.subpath
	if relPath != "/" {
		dirName = filepath.Base(relPath)
	}

	files, _ := getFileInfos(ctx.diskPath, fd.Path)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Add("Vary", "Accept-Encoding")
	out := io.Writer(w)
	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer zw.Close()
		out = zw
	}
	if err := tmpl.Execute(out, PageData{
		Crumbs:       buildCrumbs(ctx.subpath, fd.Path, relPath),
		Subpath:      ctx.subpath,
		UploadTime:   fd.UploadTime,
		DirPath:      relPath,
		DirName:      dirName,
		Files:        files,
		Entries:      listEntries(ctx.subpath, files, officeOn(ctx)),
		ParentDir:    parentDir,
		HasParentDir: ctx.diskPath != fd.Path,
		Description:  media.DirDescription(files, fd.Expiration),
		Expiration:   fd.Expiration,
		AllowPost:    fd.AllowPost,
		AllowZip:     !fd.NoZip,
		Office:       officeOn(ctx),
		OfficeEdit:   officeOn(ctx) && fd.Office == config.OfficeEdit,
		IsEmpty:      len(files) == 0,
		Text:         s.shareText[requestLang(r)],
	}); err != nil {
		GoLog.Errorf("failed to render directory template: %v", err)
	}
}

// acceptsGzip reports whether the Accept-Encoding header of r allows a gzip-compressed response.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q=")
		if !ok {
			return true
		}
		v, err := strconv.ParseFloat(q, 64)
		return err == nil && v > 0
	}
	return false
}

// zipSelection cleans the requested entry names and reports false when one escapes the directory.
func zipSelection(raw []string) ([]string, bool) {
	roots := make([]string, 0, len(raw))
	for _, f := range raw {
		clean := filepath.Clean(f)
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") {
			return nil, false
		}
		roots = append(roots, clean)
	}
	return roots, true
}

// listEntries describes files for the share page script: the link, view, icon and whether a click downloads,
// for share subpath; officeOn reports whether office documents open in the office viewer.
// It returns an empty, non-nil slice for an empty directory.
func listEntries(subpath string, files []media.FileInfo, officeOn bool) []ListEntry {
	entries := make([]ListEntry, 0, len(files))
	for _, f := range files {
		e := ListEntry{
			Name: f.Name,
			Href: pathURL("/" + subpath + f.Path),
			Size: f.Size,
			Dir:  f.IsDir,
			Icon: entryIcon(f.Name, f.IsDir),
		}
		if !f.IsDir {
			e.View = filetype.ViewFor(f.Name, officeOn)
			e.Download = !office.OpensInline(officeOn, f.Name) && filetype.Lookup(f.Name).Kind != filetype.Text
		}
		entries = append(entries, e)
	}
	return entries
}

// pathURL percent-encodes each segment of the slash-separated path p, so names with #, ? or % stay part of the path.
func pathURL(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

func buildCrumbs(subpath, sharePath, relPath string) []Crumb {
	var segments []string
	if trimmed := strings.Trim(relPath, "/"); trimmed != "" {
		segments = strings.Split(trimmed, "/")
	}

	crumbs := make([]Crumb, 0, len(segments)+1)
	dir := sharePath
	href := "/" + subpath

	for i := 0; i <= len(segments); i++ {
		name := subpath
		if i > 0 {
			name = segments[i-1]
			dir = filepath.Join(dir, name)
			href += "/" + name
		}

		c := Crumb{Name: name}
		if i < len(segments) {
			c.Href = href
			c.Siblings = subdirLinks(dir, href, segments[i])
		}
		crumbs = append(crumbs, c)
	}
	return crumbs
}

func subdirLinks(dirPath, baseHref, current string) []CrumbLink {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	links := make([]CrumbLink, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(dirPath, name))
			if err != nil {
				continue
			}
			isDir = info.IsDir()
		}
		if !isDir || !access.Readable(filepath.Join(dirPath, name)) {
			continue
		}
		links = append(links, CrumbLink{
			Name:    name,
			Href:    baseHref + "/" + name,
			Current: name == current,
		})
	}
	if len(links) < 2 {
		return nil
	}
	return links
}

// getFileInfos returns FileInfo entries for a directory, skipping the part files of unfinished uploads.
func getFileInfos(dirPath, basePath string) ([]media.FileInfo, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	infos := make([]media.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if upload.IsPart(entry.Name()) {
			continue
		}
		fullPath := filepath.Join(dirPath, entry.Name())
		if !access.Readable(fullPath) {
			continue
		}

		isDir := entry.IsDir()
		var size int64
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(fullPath); err == nil {
				isDir = info.IsDir()
				size = info.Size()
			}
		} else if info, err := entry.Info(); err == nil {
			size = info.Size()
		}
		if isDir {
			size = 0
		}

		infos = append(infos, media.FileInfo{
			Name:  entry.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), entry.Name()),
			IsDir: isDir,
			Size:  size,
		})
	}
	return infos, nil
}
