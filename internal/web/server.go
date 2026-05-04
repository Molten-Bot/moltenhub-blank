package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/moltenbot000/moltenhub-blank/internal/app"
)

//go:embed templates/index.html static
var assets embed.FS

type service interface {
	Snapshot() app.AppState
	BindAndRegister(ctx context.Context, profile app.BindProfile) error
	DisconnectAgent(ctx context.Context) error
	UpdateSettings(mutator func(*app.Settings) error) error
	SetFlash(level, message string) error
	ConsumeFlash() (app.FlashMessage, error)
}

type Server struct {
	service       service
	templates     *template.Template
	mux           *http.ServeMux
	staticHandler http.Handler
}

func New(service service) (*Server, error) {
	templates, err := template.New("index.html").Funcs(template.FuncMap{
		"formatTime": func(value time.Time) string {
			if value.IsZero() {
				return "-"
			}
			return value.Local().Format(time.RFC822)
		},
		"toJSON": func(value any) string {
			data, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				return "{}"
			}
			return string(data)
		},
	}).ParseFS(assets, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	staticAssets, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("prepare static assets: %w", err)
	}
	server := &Server{
		service:       service,
		templates:     templates,
		mux:           http.NewServeMux(),
		staticHandler: http.StripPrefix("/static/", http.FileServer(http.FS(staticAssets))),
	}
	server.routes()
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/api/onboarding", s.handleOnboarding)
	s.mux.HandleFunc("/bind", s.handleBind)
	s.mux.HandleFunc("/disconnect", s.handleDisconnect)
	s.mux.HandleFunc("/settings", s.handleSettings)
	s.mux.HandleFunc("/styles.css", s.handleStyles)
	s.mux.Handle("/static/", s.staticHandler)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.renderIndex(w, "")
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": s.service.Snapshot()})
}

func (s *Server) handleBind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	if err := s.applyRuntimeSelection(r.FormValue("hub_region")); err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	mode, bindToken, agentToken := app.NormalizeOnboardingTokens(r.FormValue("agent_mode"), r.FormValue("bind_token"), r.FormValue("agent_token"))
	handle := ""
	if mode == app.OnboardingModeNew {
		handle = strings.TrimSpace(r.FormValue("handle"))
	}
	err := s.service.BindAndRegister(r.Context(), app.BindProfile{
		AgentMode:       mode,
		AgentToken:      agentToken,
		BindToken:       bindToken,
		Handle:          handle,
		DisplayName:     strings.TrimSpace(r.FormValue("display_name")),
		Emoji:           strings.TrimSpace(r.FormValue("emoji")),
		ProfileMarkdown: strings.TrimSpace(r.FormValue("profile_markdown")),
	})
	if err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	s.redirectWithMessage(w, r, "info", "Hub connected.")
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "onboarding": onboardingViewFromState(s.service.Snapshot(), "")})
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		AgentMode       string `json:"agent_mode"`
		HubRegion       string `json:"hub_region"`
		AgentToken      string `json:"agent_token"`
		BindToken       string `json:"bind_token"`
		Handle          string `json:"handle"`
		DisplayName     string `json:"display_name"`
		Emoji           string `json:"emoji"`
		ProfileMarkdown string `json:"profile_markdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid onboarding request", "detail": err.Error()})
		return
	}
	if err := s.applyRuntimeSelection(payload.HubRegion); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error(), "detail": err.Error()})
		return
	}
	mode, bindToken, agentToken := app.NormalizeOnboardingTokens(payload.AgentMode, payload.BindToken, payload.AgentToken)
	handle := ""
	if mode == app.OnboardingModeNew {
		handle = strings.TrimSpace(payload.Handle)
	}
	err := s.service.BindAndRegister(r.Context(), app.BindProfile{
		AgentMode:       mode,
		AgentToken:      agentToken,
		BindToken:       bindToken,
		Handle:          handle,
		DisplayName:     strings.TrimSpace(payload.DisplayName),
		Emoji:           strings.TrimSpace(payload.Emoji),
		ProfileMarkdown: strings.TrimSpace(payload.ProfileMarkdown),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":         false,
			"error":      err.Error(),
			"detail":     err.Error(),
			"onboarding": onboardingViewFromState(s.service.Snapshot(), app.OnboardingStageFromError(err)),
			"bound":      strings.TrimSpace(s.service.Snapshot().Session.AgentToken) != "",
		})
		return
	}
	_ = s.service.SetFlash("info", "Hub connected.")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"message":    "Hub connected.",
		"onboarding": onboardingViewFromState(s.service.Snapshot(), "complete"),
		"bound":      true,
	})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if err := s.service.DisconnectAgent(r.Context()); err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	s.redirectWithMessage(w, r, "info", "Hub disconnected.")
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	if err := s.applyRuntimeSelection(r.FormValue("hub_region")); err != nil {
		s.redirectWithMessage(w, r, "error", err.Error())
		return
	}
	s.redirectWithMessage(w, r, "info", "Hub region updated.")
}

func (s *Server) handleStyles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := assets.ReadFile("static/styles.css")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) renderIndex(w http.ResponseWriter, flash string) {
	state := s.service.Snapshot()
	isError := false
	if flash == "" {
		if pendingFlash, err := s.service.ConsumeFlash(); err == nil {
			flash = pendingFlash.Message
			isError = strings.EqualFold(pendingFlash.Level, "error")
		}
	}
	selected, err := app.ResolveHubRuntime(state.Settings.HubRegion, state.Settings.HubURL)
	if err != nil {
		selected = app.DefaultHubRuntime()
	}
	view := pageData{
		State:           state,
		Flash:           flash,
		IsError:         isError,
		RuntimeOptions:  app.SupportedHubRuntimes(),
		SelectedRuntime: selected,
		Onboarding:      onboardingViewFromState(state, ""),
	}
	var rendered bytes.Buffer
	if err := s.templates.ExecuteTemplate(&rendered, "index.html", view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = rendered.WriteTo(w)
}

func (s *Server) applyRuntimeSelection(region string) error {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil
	}
	runtime, err := app.ResolveHubRuntime(region, "")
	if err != nil {
		return err
	}
	return s.service.UpdateSettings(func(settings *app.Settings) error {
		settings.HubRegion = runtime.ID
		settings.HubURL = runtime.HubURL
		return nil
	})
}

func (s *Server) redirectWithMessage(w http.ResponseWriter, r *http.Request, level, message string) {
	_ = s.service.SetFlash(level, message)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type pageData struct {
	State           app.AppState
	Flash           string
	IsError         bool
	RuntimeOptions  []app.HubRuntime
	SelectedRuntime app.HubRuntime
	Onboarding      onboardingView
}

type onboardingView struct {
	Steps   []app.OnboardingStep `json:"steps"`
	Message string               `json:"message,omitempty"`
	Error   bool                 `json:"error,omitempty"`
}

func onboardingViewFromState(state app.AppState, stage string) onboardingView {
	mode := app.OnboardingModeExisting
	steps := app.DefaultOnboardingStepsForMode(mode)
	if stage == "complete" || strings.TrimSpace(state.Session.AgentToken) != "" {
		for i := range steps {
			steps[i].Status = "complete"
		}
		return onboardingView{Steps: steps, Message: "Hub connected."}
	}
	for i := range steps {
		if steps[i].ID == stage {
			steps[i].Status = "error"
			return onboardingView{Steps: steps, Message: "Hub connection failed.", Error: true}
		}
	}
	return onboardingView{Steps: steps}
}
