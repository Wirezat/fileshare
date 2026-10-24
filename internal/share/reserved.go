package share

// Reserved reports whether subpath is the first path segment of a fixed route and so cannot name a share.
func Reserved(subpath string) bool {
	switch subpath {
	case "admin", "static", "setup", "healthz":
		return true
	}
	return false
}
