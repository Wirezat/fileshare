package web

import "net/http"

// chain applies middleware in order: first wraps outermost, last wraps innermost.
func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	adminRoutes := map[string]http.HandlerFunc{
		"/admin/api/shares":                            s.handleAdminShares,
		"/admin/api/shares/qr":                         s.handleAdminShareQR,
		"/admin/api/browse":                            handleAdminBrowse,
		"/admin/api/logs":                              s.handleAdminLogs,
		"/admin/api/logs/stream":                       s.handleAdminLogsStream,
		"/admin/api/settings/username":                 s.handleAdminSettingsUsername,
		"/admin/api/settings/password":                 s.handleAdminSettingsPassword,
		"/admin/api/settings/max_post_size":            s.handleAdminSettingsMaxPostSize,
		"/admin/api/settings/chunk_inactivity_timeout": s.handleAdminSettingsChunkInactivityTimeout,
		"/admin/api/settings/office":                   s.handleAdminSettingsOffice,
		"/admin/api/settings/prune_expired":            s.handleAdminFunctionPruneExpired,
		"/admin/api/uptime":                            s.handleAdminUptime,

		"/admin":          s.handleAdminUI,
		"/admin/logs":     s.handleAdminLogsPage,
		"/admin/settings": s.handleAdminSettingsPage,
		"/admin/api/me":   s.handleAdminMe,
	}
	for path, h := range adminRoutes {
		mux.HandleFunc(path, s.adminAuth(h))
	}

	uiFS := http.StripPrefix("/static/ui/", http.FileServer(http.Dir(s.asset("ui"))))
	mux.Handle("/static/ui/", uiFS)
	mux.HandleFunc("/static/theme.css", s.handleThemeCSS)

	jsFS := http.StripPrefix("/admin/static/js/", http.FileServer(http.Dir(s.asset("js/admin"))))
	mux.HandleFunc("/admin/static/js/", s.adminAuth(jsFS.ServeHTTP))

	mux.HandleFunc("/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/admin/logout", s.handleAdminLogout)

	mux.HandleFunc("GET /setup", s.handleSetupUI)
	mux.HandleFunc("POST /setup/api/init", s.handleSetupInit)

	mux.HandleFunc("POST /{subpath}/chunk-init", s.handleChunkInit)
	mux.HandleFunc("POST /{subpath}/chunk", s.handleChunkReceive)

	mux.HandleFunc("POST /{subpath}/unlock", s.handleUnlock)

	public := chain(
		http.HandlerFunc(s.handleRequest),
		s.loggingMiddleware,
	)
	mux.Handle("/", public)
	mux.HandleFunc("/static/share.css", s.handleShareCSS)
	mux.HandleFunc("/static/share.js", s.handleShareJS)

	mux.Handle("/static/locales/", http.StripPrefix("/static/locales/", http.FileServer(http.Dir(s.asset("locales")))))

	return mux
}
