package web

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dshills/speccritic/internal/app"
	"github.com/dshills/speccritic/internal/render"
	"github.com/dshills/speccritic/internal/schema"
)

type indexData struct {
	Config Config
	Nonce  string
}

const maxWebTokens = 16384
const multipartMemoryLimit = 1 << 20
const multipartOverheadLimit = 1 << 20

type resultView struct {
	Check     *StoredCheck
	Annotated AnnotatedSpec
	Issues    []schema.Issue
	Questions []schema.Question
}

type findingDetail struct {
	CheckID  string
	Issue    *schema.Issue
	Question *schema.Question
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

func sanitizeWebError(err error) string {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		switch appErr.Kind {
		case app.ErrorInput:
			return appErr.Error()
		case app.ErrorProvider:
			return "LLM provider error."
		case app.ErrorModelOutput:
			return "Invalid model output."
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Request timed out."
	}
	return "Internal server error."
}

func parseSeverity(raw string) schema.Severity {
	switch strings.ToLower(raw) {
	case "warn":
		return schema.SeverityWarn
	case "critical":
		return schema.SeverityCritical
	default:
		return schema.SeverityInfo
	}
}

func filterIssues(issues []schema.Issue, threshold schema.Severity) []schema.Issue {
	out := make([]schema.Issue, 0, len(issues))
	for _, issue := range issues {
		if handlerMeetsThreshold(issue.Severity, threshold) {
			out = append(out, issue)
		}
	}
	return out
}

func filterQuestions(questions []schema.Question, threshold schema.Severity) []schema.Question {
	out := make([]schema.Question, 0, len(questions))
	for _, question := range questions {
		if handlerMeetsThreshold(question.Severity, threshold) {
			out = append(out, question)
		}
	}
	return out
}

func handlerMeetsThreshold(severity, threshold schema.Severity) bool {
	return handlerSeverityRank(severity) >= handlerSeverityRank(threshold)
}

func handlerSeverityRank(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 2
	case schema.SeverityWarn:
		return 1
	case schema.SeverityInfo:
		return 0
	default:
		return -1
	}
}

func (s *Server) handleCheckStub(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxUploadBytes+multipartOverheadLimit)
	if err := s.parseRequestForm(r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !s.validNonce(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	req, err := s.parseCheckRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	result, err := s.checker.Check(ctx, req)
	if err != nil {
		log.Printf("check failed: %v", err)
		status := http.StatusInternalServerError
		var appErr *app.Error
		if errors.As(err, &appErr) {
			switch appErr.Kind {
			case app.ErrorInput:
				status = http.StatusBadRequest
			case app.ErrorProvider, app.ErrorModelOutput:
				status = http.StatusBadGateway
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, sanitizeWebError(err), status)
		return
	}
	stored, err := s.store.Save(result)
	if err != nil {
		log.Printf("store check: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	view, err := s.resultView(stored)
	if err != nil {
		log.Printf("build result view: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "partial_result.html", view); err != nil {
		log.Printf("render result: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleIssueDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	findingID := r.PathValue("finding_id")
	check, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if check.Result == nil || check.Result.Report == nil {
		http.NotFound(w, r)
		return
	}
	detail := findingDetail{CheckID: id}
	for i := range check.Result.Report.Issues {
		if check.Result.Report.Issues[i].ID == findingID {
			detail.Issue = &check.Result.Report.Issues[i]
			break
		}
	}
	if detail.Issue == nil {
		for i := range check.Result.Report.Questions {
			if check.Result.Report.Questions[i].ID == findingID {
				detail.Question = &check.Result.Report.Questions[i]
				break
			}
		}
	}
	if detail.Issue == nil && detail.Question == nil {
		http.NotFound(w, r)
		return
	}
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "partial_issue_detail.html", detail); err != nil {
		log.Printf("render issue detail: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	s.handleExport(w, r, "json", "application/json", ".json")
}

func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request) {
	s.handleExport(w, r, "md", "text/markdown; charset=utf-8", ".md")
}

func (s *Server) handleExportPatch(w http.ResponseWriter, r *http.Request) {
	check, ok := s.store.Get(r.PathValue("id"))
	if !ok || check.Result == nil || check.Result.PatchDiff == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/x-diff; charset=utf-8")
	w.Header().Set("Content-Disposition", contentDisposition("speccritic-"+check.ID+".diff"))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, check.Result.PatchDiff)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request, format, contentType, ext string) {
	check, ok := s.store.Get(r.PathValue("id"))
	if !ok || check.Result == nil || check.Result.Report == nil {
		http.NotFound(w, r)
		return
	}
	renderer, err := render.NewRenderer(format)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data, err := renderer.Render(check.Result.Report)
	if err != nil {
		log.Printf("render export: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition("speccritic-"+check.ID+ext))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) resultView(check *StoredCheck) (resultView, error) {
	if check == nil || check.Result == nil || check.Result.Report == nil {
		return resultView{}, fmt.Errorf("missing check result")
	}
	threshold := parseSeverity(check.Result.Report.Input.SeverityThreshold)
	annotated, err := buildAnnotatedSpec(check.Result.OriginalSpec, check.Result.Report, threshold)
	if err != nil {
		return resultView{}, err
	}
	return resultView{
		Check:     check,
		Annotated: annotated,
		Issues:    filterIssues(check.Result.Report.Issues, threshold),
		Questions: filterQuestions(check.Result.Report.Questions, threshold),
	}, nil
}

func buildAnnotatedSpec(specText string, report *schema.Report, threshold schema.Severity) (AnnotatedSpec, error) {
	return BuildAnnotatedSpec(specText, report, threshold)
}

func contentDisposition(name string) string {
	return `attachment; filename="` + sanitizeFilename(name) + `"`
}

func sanitizeFilename(name string) string {
	if name == "" {
		return "speccritic"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "speccritic"
	}
	return b.String()
}

func (s *Server) parseRequestForm(r *http.Request) error {
	isMultipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
	if isMultipart {
		err := r.ParseMultipartForm(multipartMemoryLimit)
		if r.MultipartForm != nil && err != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		if err != nil {
			return fmt.Errorf("invalid form: %w", err)
		}
		return nil
	}
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("invalid form: %w", err)
	}
	return nil
}

func (s *Server) parseCheckRequest(r *http.Request) (app.CheckRequest, error) {
	isMultipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
	specText := strings.TrimSpace(r.FormValue("spec_text"))
	specName := "SPEC.md"
	var file multipart.File
	var header *multipart.FileHeader
	hasFile := false
	if isMultipart {
		var fileErr error
		file, header, fileErr = r.FormFile("spec_file")
		hasFile = fileErr == nil
		if fileErr != nil && !errors.Is(fileErr, http.ErrMissingFile) {
			return app.CheckRequest{}, fmt.Errorf("reading uploaded file: %w", fileErr)
		}
	}
	if hasFile {
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, s.config.MaxUploadBytes+1))
		if err != nil {
			return app.CheckRequest{}, fmt.Errorf("reading uploaded file: %w", err)
		}
		if int64(len(data)) > s.config.MaxUploadBytes {
			return app.CheckRequest{}, fmt.Errorf("uploaded file exceeds %d bytes", s.config.MaxUploadBytes)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return app.CheckRequest{}, fmt.Errorf("spec is empty")
		}
		if !utf8.Valid(data) {
			return app.CheckRequest{}, fmt.Errorf("uploaded file must be UTF-8 text")
		}
		if specText != "" {
			return app.CheckRequest{}, fmt.Errorf("provide pasted text or uploaded file, not both")
		}
		specText = string(data)
		if header != nil && header.Filename != "" {
			specName = filepath.Base(header.Filename)
		}
	}
	if specText == "" {
		return app.CheckRequest{}, fmt.Errorf("spec is required")
	}

	profile := r.FormValue("profile")
	if profile == "" {
		profile = "general"
	}
	switch profile {
	case "general", "backend-api", "regulated-system", "event-driven":
	default:
		return app.CheckRequest{}, fmt.Errorf("invalid profile %q", profile)
	}

	severity := r.FormValue("severity_threshold")
	if severity == "" {
		severity = "info"
	}
	switch severity {
	case "info", "warn", "critical":
	default:
		return app.CheckRequest{}, fmt.Errorf("invalid severity threshold %q", severity)
	}

	temperature := 0.2
	if raw := r.FormValue("temperature"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 2 {
			return app.CheckRequest{}, fmt.Errorf("invalid temperature")
		}
		temperature = v
	}

	maxTokens := 16384
	if raw := r.FormValue("max_tokens"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 || v > maxWebTokens {
			return app.CheckRequest{}, fmt.Errorf("invalid max tokens")
		}
		maxTokens = v
	}

	return app.CheckRequest{
		SpecName:          specName,
		SpecText:          specText,
		Profile:           profile,
		Strict:            r.FormValue("strict") == "true",
		SeverityThreshold: severity,
		Temperature:       temperature,
		MaxTokens:         maxTokens,
		Source:            app.SourceWeb,
		ErrWriter:         io.Discard,
	}, nil
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
	submitted := r.PostFormValue("csrf_token")
	if submitted == "" || len(submitted) != len(formCookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(formCookie.Value)) == 1
}
