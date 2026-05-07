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

	files, _ := getFileInfos(ctx.diskPath, fd.Path)

	if err := tmpl.Execute(w, PageData{
		Subpath:      ctx.subpath,
		UploadTime:   fd.UploadTime,
		DirPath:      relPath,
		Files:        files,
		ParentDir:    parentDir,
		HasParentDir: ctx.diskPath != fd.Path,
		Uses:         fd.Uses,
		Expiration:   fd.Expiration,
		AllowPost:    fd.AllowPost,
	}); err != nil {
		GoLog.Errorf("failed to render directory template: %v", err)
	}
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
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(fullPath); err == nil {
				isDir = info.IsDir()
			}
		}

		infos = append(infos, shared.FileInfo{
			Name:  entry.Name(),
			Path:  filepath.Join("/", strings.TrimPrefix(dirPath, basePath), entry.Name()),
			IsDir: isDir,
		})
	}
	return infos, nil
}

// loadTemplate parses the directory template once and reuses it for all listings.
func loadTemplate() (*template.Template, error) {
	dirTemplateOnce.Do(func() {
		dirTemplate, dirTemplateErr = template.New("directory").
			Funcs(template.FuncMap{
				"getFileExtension": func(name string) string {
					return strings.ToLower(filepath.Ext(name))
				},
			}).
			ParseFiles(shareHtmlPath)
		if dirTemplateErr != nil {
			GoLog.Errorf("failed to parse directory template: %v", dirTemplateErr)
		}
	})
	return dirTemplate, dirTemplateErr
}
