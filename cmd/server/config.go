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
// computed once by prepareRequest and passed to the individual handlers.
type requestContext struct {
	config   shared.JsonData
	fileData shared.FileData
	subpath  string
	diskPath string
	fileInfo os.FileInfo
}

// PageData contains all data needed to render the directory view template
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
