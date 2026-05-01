package web

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html assets/*
var content embed.FS

type Server struct {
	config    Config
	templates *template.Template
	handler   http.Handler
}

func NewServer(config Config) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &Server{
		config:    config,
		templates: tmpl,
	}
	s.handler = s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}
