package web

import (
	"os"

	"github.com/Wirezat/fileshare/internal/config"
	"github.com/Wirezat/fileshare/internal/media"
)

// requestContext holds the resolved share and file of one request.
type requestContext struct {
	config   config.View
	fileData config.FileData
	subpath  string
	diskPath string
	fileInfo os.FileInfo
}

// PageData contains all fields required to render the directory listing template.
type PageData struct {
	Subpath      string
	UploadTime   int64
	DirPath      string
	DirName      string
	Crumbs       []Crumb
	Files        []media.FileInfo
	ParentDir    string
	HasParentDir bool
	Description  string
	Expiration   int64
	AllowPost    bool
	AllowZip     bool
	Office       bool
	IsEmpty      bool
}

type Crumb struct {
	Name     string
	Href     string
	Siblings []CrumbLink
}

type CrumbLink struct {
	Name    string
	Href    string
	Current bool
}
