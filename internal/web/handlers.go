package web

import (
	"bytes"
	"crypto/subtle"
	"log"
	"net/http"
)

type indexData struct {
	Config Config
	Nonce  string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	nonce := ""
	sessionCookie, sessionErr := r.Cookie("speccritic_session")
	formCookie, formErr := r.Cookie("speccritic_form")
	if formErr == nil {
		nonce = formCookie.Value
	}
	createdNonce := false
	if nonce == "" {
		var err error
		nonce, err = newNonce()
		if err != nil {
			log.Printf("generate request nonce: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		createdNonce = true
	}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "layout.html", indexData{
		Config: s.config,
		Nonce:  nonce,
	}); err != nil {
		log.Printf("render index: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if createdNonce {
		http.SetCookie(w, &http.Cookie{
			Name:     "speccritic_form",
			Value:    nonce,
			Path:     "/",
			HttpOnly: true,
			Secure:   isSecureRequest(r),
			SameSite: http.SameSiteStrictMode,
		})
	}
	if sessionErr != nil || sessionCookie.Value == "" {
		sessionValue, err := newNonce()
		if err != nil {
			log.Printf("generate session nonce: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "speccritic_session",
			Value:    sessionValue,
			Path:     "/",
			HttpOnly: true,
			Secure:   isSecureRequest(r),
			SameSite: http.SameSiteStrictMode,
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return
	}
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *Server) handleCheckStub(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxUploadBytes)
	if !s.validNonce(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	http.Error(w, "Check submission is not implemented yet.", http.StatusNotImplemented)
}

func (s *Server) validNonce(r *http.Request) bool {
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete && r.Method != http.MethodPatch {
		return false
	}
	sessionCookie, err := r.Cookie("speccritic_session")
	if err != nil || sessionCookie.Value == "" {
		return false
	}
	formCookie, err := r.Cookie("speccritic_form")
	if err != nil || formCookie.Value == "" {
		return false
	}
	if err := r.ParseForm(); err != nil {
		return false
	}
	submitted := r.PostFormValue("csrf_token")
	if submitted == "" || len(submitted) != len(formCookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(formCookie.Value)) == 1
}
