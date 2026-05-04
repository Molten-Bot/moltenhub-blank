package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moltenbot000/moltenhub-blank/internal/app"
)

type fakeService struct {
	state       app.AppState
	bindProfile app.BindProfile
}

func (f *fakeService) Snapshot() app.AppState {
	return f.state
}

func (f *fakeService) BindAndRegister(_ context.Context, profile app.BindProfile) error {
	f.bindProfile = profile
	return nil
}

func (f *fakeService) DisconnectAgent(context.Context) error {
	return nil
}

func (f *fakeService) UpdateSettings(mutator func(*app.Settings) error) error {
	return mutator(&f.state.Settings)
}

func (f *fakeService) SetFlash(level, message string) error {
	f.state.Flash = app.FlashMessage{Level: level, Message: message}
	return nil
}

func (f *fakeService) ConsumeFlash() (app.FlashMessage, error) {
	flash := f.state.Flash
	f.state.Flash = app.FlashMessage{}
	return flash, nil
}

func TestHandleBindSubmitsBindTokenWithHandle(t *testing.T) {
	t.Parallel()

	runtime := app.DefaultHubRuntime()
	service := &fakeService{state: app.AppState{Settings: app.DefaultSettings()}}
	server, err := New(service)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	form := url.Values{
		"hub_region":       {runtime.ID},
		"agent_mode":       {app.OnboardingModeNew},
		"bind_token":       {"bind-123"},
		"handle":           {"new-agent"},
		"display_name":     {"Molten Hub Blank"},
		"emoji":            {"*"},
		"profile_markdown": {"Blank Molten Hub connectivity app."},
	}
	req := httptest.NewRequest(http.MethodPost, "/bind", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := service.bindProfile.AgentMode; got != app.OnboardingModeNew {
		t.Fatalf("agent_mode = %q, want %q", got, app.OnboardingModeNew)
	}
	if got := service.bindProfile.BindToken; got != "bind-123" {
		t.Fatalf("bind_token = %q, want bind-123", got)
	}
	if got := service.bindProfile.Handle; got != "new-agent" {
		t.Fatalf("handle = %q, want new-agent", got)
	}
	if got := service.bindProfile.AgentToken; got != "" {
		t.Fatalf("agent_token = %q, want empty", got)
	}
}
