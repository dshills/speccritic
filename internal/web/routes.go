package web

import "net/http"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /checks", s.handleCheckStub)
	mux.HandleFunc("GET /checks/{id}/issues/{finding_id}", s.handleIssueDetail)
	mux.HandleFunc("GET /checks/{id}/export.json", s.handleExportJSON)
	mux.HandleFunc("GET /checks/{id}/export.md", s.handleExportMarkdown)
	mux.HandleFunc("GET /checks/{id}/patch.diff", s.handleExportPatch)
	mux.Handle("GET /assets/", http.FileServer(http.FS(content)))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
