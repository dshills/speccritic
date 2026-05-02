package web

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"os"

	"github.com/dshills/speccritic/internal/app"
)

//go:embed templates/*.html assets/*
var content embed.FS

type Server struct {
	config    Config
	checker   checker
	editor    editorOpener
	editorOK  bool
	store     *Store
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
	config = withDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"isPreflight": isPreflightTags,
	}).ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, err
	}
	store, err := NewStore(config.MaxRetainedChecks, config.RetainedCheckTTL)
	if err != nil {
		return nil, err
	}
	editorRoot, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	s := &Server{
		config:    config,
		checker:   c,
		editor:    externalEditorOpener{root: editorRoot},
		editorOK:  addrIsLoopback(config.Addr),
		store:     store,
		templates: tmpl,
	}
	s.handler = s.routes()
	return s, nil
}

func withDefaults(config Config) Config {
	defaults := DefaultConfig()
	if config.Addr == "" {
		config.Addr = defaults.Addr
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = defaults.RequestTimeout
	}
	if config.MaxUploadBytes == 0 {
		config.MaxUploadBytes = defaults.MaxUploadBytes
	}
	if config.MaxRetainedChecks == 0 {
		config.MaxRetainedChecks = defaults.MaxRetainedChecks
	}
	if config.RetainedCheckTTL == 0 {
		config.RetainedCheckTTL = defaults.RetainedCheckTTL
	}
	return config
}

func (s *Server) Handler() http.Handler {
	return s.handler
}
