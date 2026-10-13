package shared

import "time"

// IsExpired reports whether a share has expired due to manual expiry
// or a passed expiration timestamp.
func IsExpired(fd FileData) bool {
	return fd.Expired || (fd.Expiration != 0 && fd.Expiration < time.Now().Unix())
}
