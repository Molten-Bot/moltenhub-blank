package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moltenbot000/moltenhub-blank/internal/app"
)

type fakeService struct {
	state app.AppState
}

func (f *fakeService) Snapshot() app.AppState {
	return f.state
}

func (f *fakeService) BindAndRegister(context.Context, app.BindProfile) error {
	return nil
}

func (f *fakeService) DisconnectAgent(context.Context) error {
	return nil
}

func (f *fakeService) UpdateSettings(func(*app.Settings) error) error {
	return nil
}

func (f *fakeService) SetFlash(string, string) error {
	return nil
}

func (f *fakeService) ConsumeFlash() (app.FlashMessage, error) {
	return app.FlashMessage{}, nil
}

func TestIndexHidesDisconnectUntilConnected(t *testing.T) {
	server, err := New(&fakeService{state: app.AppState{
		Settings: app.DefaultSettings(),
		Session: app.Session{
			AgentToken: "t_agent",
		},
		Connection: app.ConnectionState{
			Status:    app.ConnectionStatusDisconnected,
			Transport: app.ConnectionTransportOffline,
		},
	}})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "Disconnect") {
		t.Fatalf("disconnect button rendered before agent connected")
	}
}

func TestIndexShowsDisconnectWhenConnected(t *testing.T) {
	server, err := New(&fakeService{state: app.AppState{
		Settings: app.DefaultSettings(),
		Session: app.Session{
			AgentToken: "t_agent",
		},
		Connection: app.ConnectionState{
			Status:    app.ConnectionStatusConnected,
			Transport: app.ConnectionTransportHTTP,
		},
	}})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "Disconnect") {
		t.Fatalf("disconnect button was not rendered for connected agent")
	}
}
