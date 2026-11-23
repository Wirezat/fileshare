package web

import (
	"os"

	"github.com/Wirezat/fileshare/internal/config"
)

// requestContext holds the resolved share and file of one request.
type requestContext struct {
	config   config.View
	fileData config.FileData
	subpath  string
	diskPath string
	fileInfo os.FileInfo
	machine  bool
}

// ListEntry is one directory entry as the share page script reads it.
type ListEntry struct {
	Name     string  `json:"name"`
	Href     string  `json:"href"`
	Size     int64   `json:"size"`
	Dir      bool    `json:"dir,omitempty"`
	View     string  `json:"view,omitempty"`
	Icon     string  `json:"icon"`
	Download bool    `json:"dl,omitempty"`
	Ratio    float64 `json:"ar,omitempty"`
}

// PageData contains all fields required to render the directory listing template.
type PageData struct {
	Subpath      string
	UploadTime   int64
	DirPath      string
	DirName      string
	Crumbs       []Crumb
	Entries      []ListEntry
	ParentDir    string
	HasParentDir bool
	Description  string
	Expiration   int64
	AllowPost    bool
	AllowZip     bool
	Office       bool
	OfficeEdit   bool
	TextEdit     bool
	IsEmpty      bool
	Text         map[string]string
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
