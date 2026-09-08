package main

import (
	"os"

	"github.com/Wirezat/fileshare/pkg/shared"
)

const (
	shareHtmlPath = "./web/html/share.html"
	shareCssPath  = "./web/css/share.css"
	shareJsPath   = "./web/js/share.js"
	setupHtmlPath = "./web/html/setup.html"
	gateHtmlPath  = "./web/html/gate.html"

	adminHtmlPath         = "./web/html/admin.html"
	adminLogsHtmlPath     = "./web/html/admin-logs.html"
	adminSettingsHtmlPath = "./web/html/admin-settings.html"
	adminLoginHtmlPath    = "./web/html/admin-login.html"
	adminJsDir            = "./web/js/admin"
	themeCssPath          = "./web/css/theme.css"
	uiDir                 = "./web/ui"
	localesDir            = "./web/locales"
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
	IsEmpty      bool
}
