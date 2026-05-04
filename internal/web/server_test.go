package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/moltenbot000/moltenhub-blank/internal/app"
)

type stubService struct {
	state app.AppState
}

func (s *stubService) Snapshot() app.AppState {
	return s.state
}

func (s *stubService) BindAndRegister(context.Context, app.BindProfile) error {
	return nil
}

func (s *stubService) DisconnectAgent(context.Context) error {
	return nil
}

func (s *stubService) UpdateSettings(mutator func(*app.Settings) error) error {
	return mutator(&s.state.Settings)
}

func (s *stubService) SetFlash(level, message string) error {
	s.state.Flash = app.FlashMessage{Level: level, Message: message}
	return nil
}

func (s *stubService) ConsumeFlash() (app.FlashMessage, error) {
	flash := s.state.Flash
	s.state.Flash = app.FlashMessage{}
	return flash, nil
}

func TestIndexRendersDispatchEmojiPicker(t *testing.T) {
	settings := app.DefaultSettings()
	service := &stubService{
		state: app.AppState{
			Settings: settings,
			Session: app.Session{
				Emoji: "\U0001F680",
			},
			Connection: app.ConnectionState{
				Status:    app.ConnectionStatusDisconnected,
				Transport: app.ConnectionTransportOffline,
			},
		},
	}
	server, err := New(service)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`class="hub-emoji-picker" data-hub-emoji-picker id="agent-settings-emoji-picker"`,
		`type="hidden" name="emoji" value="` + "\U0001F680" + `" data-hub-emoji-input`,
		`data-hub-emoji-toggle`,
		`aria-label="Choose emoji"`,
		`data-hub-emoji-panel hidden`,
		`data-hub-emoji-mart-root`,
		`const initHubEmojiPicker = (root) => {`,
		`document.body.appendChild(panel);`,
		`const PROFILE_EMOJI_GROUPS = [`,
		`className = "hub-emoji-picker-category"`,
		`className = "hub-emoji-picker-grid"`,
		`label: "Agent"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q", want)
		}
	}
	for _, unwanted := range []string{
		`hub-emoji-picker-toggle-text`,
		`hub-emoji-picker-toggle-caret`,
		`data-hub-emoji-selected-text`,
		`type="radio" name="emoji"`,
		`https://esm.sh/@emoji-mart`,
		`Loading emoji picker...`,
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("index unexpectedly included %q", unwanted)
		}
	}
}

func TestEmojiPickerPanelStacksAboveModalBackdrop(t *testing.T) {
	styles, err := os.ReadFile("static/styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}

	content := string(styles)
	if !strings.Contains(content, ".hub-emoji-picker-panel {\n  position: fixed;\n  z-index: 130;") {
		t.Fatalf("expected emoji picker panel to sit above modal backdrops")
	}
	if !strings.Contains(content, ".settings-modal-backdrop,\n.onboarding-modal-backdrop {\n  position: fixed;\n  inset: 0;\n  z-index: 121;") {
		t.Fatalf("expected modal backdrop z-index baseline")
	}
}
