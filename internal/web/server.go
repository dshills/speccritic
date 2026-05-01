package web

import (
	"context"
	"embed"
	"html/template"
	"net/http"

	"github.com/dshills/speccritic/internal/app"
)

//go:embed templates/*.html assets/*
var content embed.FS

type Server struct {
	config    Config
	checker   checker
	templates *template.Template
	handler   http.Handler
}

func NewServer(config Config) (*Server, error) {
	return NewServerWithChecker(config, app.NewChecker())
}

type checker interface {
	Check(rctx context.Context, req app.CheckRequest) (*app.CheckResult, error)
}

func NewServerWithChecker(config Config, c checker) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &Server{
		config:    config,
		checker:   c,
		templates: tmpl,
	}
	s.handler = s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}
