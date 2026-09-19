package main

import (
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/pkg/shared"
)

var (
	dirTemplate     *template.Template
	dirTemplateErr  error
	dirTemplateOnce sync.Once
)

// serveDirectory renders the directory listing, or streams a ZIP if ?download=zip.
func serveDirectory(w http.ResponseWriter, r *http.Request, ctx *requestContext) {
	if r.URL.Query().Get("download") == "zip" {
		if ctx.fileData.NoZip {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		zipAndServe(w, ctx.diskPath)
		return
	}

	tmpl, err := loadTemplate()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

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

	if err := tmpl.Execute(w, PageData{
		Crumbs:       buildCrumbs(ctx.subpath, fd.Path, relPath),
		Subpath:      ctx.subpath,
		UploadTime:   fd.UploadTime,
		DirPath:      relPath,
		DirName:      dirName,
		Files:        files,
		ParentDir:    parentDir,
		HasParentDir: ctx.diskPath != fd.Path,
		Uses:         fd.Uses,
		Expiration:   fd.Expiration,
		AllowPost:    fd.AllowPost,
		AllowZip:     !fd.NoZip,
		Office:       fd.Office != shared.OfficeOff && ctx.config.OfficeURL != "" && ctx.config.OfficeSecret != "",
		IsEmpty:      len(files) == 0,
	}); err != nil {
		GoLog.Errorf("failed to render directory template: %v", err)
	}
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
		if strings.HasPrefix(name, ".") {
			continue
		}
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(dirPath, name))
			if err != nil {
				continue
			}
			isDir = info.IsDir()
		}
		if !isDir {
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

// getFileInfos returns FileInfo entries for a directory, skipping hidden files.
func getFileInfos(dirPath, basePath string) ([]shared.FileInfo, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	infos := make([]shared.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fullPath := filepath.Join(dirPath, entry.Name())

		isDir := entry.IsDir()
		var size int64
		if entry.Type()&fs.ModeSymlink != 0 {
			// Stat follows the link, so both fields describe the target.
			if info, err := os.Stat(fullPath); err == nil {
				isDir = info.IsDir()
				size = info.Size()
			}
		} else if info, err := entry.Info(); err == nil {
			size = info.Size()
		}
		if isDir {
			size = 0 // a directory's own inode size means nothing to a listing
		}

		infos = append(infos, shared.FileInfo{
			Name:  entry.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), entry.Name()),
			IsDir: isDir,
			Size:  size,
		})
	}
	return infos, nil
}

// loadTemplate parses the directory template once and reuses it for all listings.
func loadTemplate() (*template.Template, error) {
	dirTemplateOnce.Do(func() {
		dirTemplate, dirTemplateErr = template.New("directory").
			Funcs(template.FuncMap{"opensInline": opensInline}).
			ParseFiles(shareHtmlPath)
		if dirTemplateErr != nil {
			GoLog.Errorf("failed to parse directory template: %v", dirTemplateErr)
		}
	})
	return dirTemplate, dirTemplateErr
}
