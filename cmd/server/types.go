package main

import (
	"os"

	"github.com/Wirezat/fileshare/pkg/shared"
)

const (
	shareHtmlPath = "./web/html/share.html"
	shareCssPath  = "./web/css/share.css"
	shareJsPath   = "./web/js/share.js"
	adminHtmlPath = "./web/html/admin.html"
	adminCssPath  = "./web/css/admin.css"
	adminJsPath   = "./web/js/admin.js"
)

// requestContext holds all resolved data for an incoming request,
// populated once by prepareRequest and passed down to method handlers.
type requestContext struct {
	config   *shared.Config
	fileData shared.FileData
	subpath  string
	diskPath string
	fileInfo os.FileInfo
}

// PageData contains all fields required to render the directory listing template.
type PageData struct {
	Subpath      string
	UploadTime   int64
	DirPath      string
	Files        []shared.FileInfo
	ParentDir    string
	HasParentDir bool
	Uses         int
	Expiration   int64
	AllowPost    bool
}
