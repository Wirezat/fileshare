package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Wirezat/GoLog"
)

func tooManyAttempts(w http.ResponseWriter, wait time.Duration, client, what string) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
	GoLog.Warnf("%s: rate limited %s for %ds", what, GoLog.IP(client), secs)
}
