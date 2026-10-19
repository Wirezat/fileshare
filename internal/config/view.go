package config

import "iter"

// View is a read-only handle on one immutable Config snapshot.
type View struct{ c *Config }

// NewView wraps c; c must not be modified afterwards.
func NewView(c *Config) View { return View{c: c} }

func (v View) Port() int                   { return v.c.Port }
func (v View) MaxPostSize() int            { return v.c.MaxPostSize }
func (v View) ChunkInactivityTimeout() int { return v.c.ChunkInactivityTimeout }
func (v View) AdminUsername() string       { return v.c.AdminUsername }
func (v View) AdminPassword() string       { return v.c.AdminPassword }
func (v View) OfficeURL() string           { return v.c.OfficeURL }
func (v View) OfficeSecret() string        { return v.c.OfficeSecret }

func (v View) DownloadLimit() (enabled bool, mbit int) {
	return v.c.DownloadLimitEnabled, v.c.DownloadLimitMbit
}

func (v View) OfficeReady() bool { return v.c.OfficeURL != "" && v.c.OfficeSecret != "" }

func (v View) Share(subpath string) (FileData, bool) {
	fd, ok := v.c.Files[subpath]
	return fd, ok
}

func (v View) Shares() iter.Seq2[string, FileData] {
	return func(yield func(string, FileData) bool) {
		for sub, fd := range v.c.Files {
			if !yield(sub, fd) {
				return
			}
		}
	}
}

func (v View) ShareCount() int { return len(v.c.Files) }
