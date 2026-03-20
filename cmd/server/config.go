package main

import (
	"os"

	"github.com/Wirezat/fileshare/pkg/shared"
)

const (
	templateFilePath = "./template.html"
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
