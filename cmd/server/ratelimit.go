package main

import (
	"net/http"
	"strconv"

	"github.com/Wirezat/GoLog"
	"github.com/Wirezat/fileshare/internal/ratelimit"
)

var (
	loginLimiter  = ratelimit.New()
	unlockLimiter = ratelimit.New()
)

// allow writes a 429 and reports false when key is out of attempts; what names the endpoint in the log line.
func allow(l *ratelimit.Limiter, w http.ResponseWriter, key, what string) bool {
	wait := l.RetryAfter(key)
	if wait <= 0 {
		return true
	}
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
	GoLog.Warnf("%s: rate limited %s for %ds", what, key, secs)
	return false
}
