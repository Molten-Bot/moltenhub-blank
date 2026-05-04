package app

import (
	"context"
	"testing"
	"time"

	"github.com/moltenbot000/moltenhub-blank/internal/hub"
)

type fakeHubClient struct {
	bindRequests       []hub.BindRequest
	metadataRequests   []hub.UpdateMetadataRequest
	capabilitiesTokens []string
	bindResponse       hub.BindResponse
	capabilities       map[string]any
}

func (f *fakeHubClient) BindAgent(_ context.Context, req hub.BindRequest) (hub.BindResponse, error) {
	f.bindRequests = append(f.bindRequests, req)
	if f.bindResponse.AgentToken == "" {
		f.bindResponse.AgentToken = "t_bound"
	}
	return f.bindResponse, nil
}

func (f *fakeHubClient) UpdateMetadata(_ context.Context, _ string, req hub.UpdateMetadataRequest) (map[string]any, error) {
	f.metadataRequests = append(f.metadataRequests, req)
	return map[string]any{"ok": true}, nil
}

func (f *fakeHubClient) GetCapabilities(_ context.Context, token string) (map[string]any, error) {
	f.capabilitiesTokens = append(f.capabilitiesTokens, token)
	if f.capabilities != nil {
		return f.capabilities, nil
	}
	return map[string]any{}, nil
}

func (f *fakeHubClient) PullOpenClaw(context.Context, string, time.Duration) (hub.PullResponse, bool, error) {
	return hub.PullResponse{}, false, nil
}

func (f *fakeHubClient) AckOpenClaw(context.Context, string, string) error {
	return nil
}

func (f *fakeHubClient) NackOpenClaw(context.Context, string, string) error {
	return nil
}

func (f *fakeHubClient) MarkOffline(context.Context, string, hub.OfflineRequest) error {
	return nil
}

func TestEnvValueSupportsColonStyleToken(t *testing.T) {
	t.Setenv(moltenHubTokenEnvVar+":t_env-agent-123", "")

	value, ok := envValue(moltenHubTokenEnvVar)
	if !ok || value != "t_env-agent-123" {
		t.Fatalf("envValue() = %q, %v; want t_env-agent-123, true", value, ok)
	}
}

func TestEnvValuePreservesHostPortValues(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "127.0.0.1:18080")
	t.Setenv(moltenHubTokenEnvVar, moltenHubTokenEnvVar+":t_inline-agent")

	if got := envOrDefault("LISTEN_ADDR", ":8080"); got != "127.0.0.1:18080" {
		t.Fatalf("LISTEN_ADDR = %q, want 127.0.0.1:18080", got)
	}
	value, ok := envValue(moltenHubTokenEnvVar)
	if !ok || value != "t_inline-agent" {
		t.Fatalf("envValue() = %q, %v; want t_inline-agent, true", value, ok)
	}
}

func TestConnectFromEnvSkipsBindTokenWhenAlreadyBound(t *testing.T) {
	pinFallbackHubRuntimes(t)
	t.Setenv(moltenHubTokenEnvVar, "b_bind-123")
	t.Setenv(moltenHubRegionEnvVar, HubRegionEU)

	service, fake, store := newServiceForTest(t, func(state *AppState) {
		state.Settings.HubRegion = HubRegionNA
		state.Settings.HubURL = hubURLForRegion(HubRegionNA)
		state.Session.AgentToken = "persisted-token"
		state.Session.BindToken = "persisted-token"
	})

	if err := service.ConnectFromEnvIfNeeded(context.Background()); err != nil {
		t.Fatalf("connect from env: %v", err)
	}
	if len(fake.bindRequests) != 0 {
		t.Fatalf("bind requests = %d, want 0", len(fake.bindRequests))
	}
	if len(fake.capabilitiesTokens) != 0 {
		t.Fatalf("capability checks = %d, want 0", len(fake.capabilitiesTokens))
	}
	state := store.Snapshot()
	if got := state.Settings.HubRegion; got != HubRegionNA {
		t.Fatalf("hub_region = %q, want %q", got, HubRegionNA)
	}
}

func TestConnectFromEnvRevalidatesExistingAgentToken(t *testing.T) {
	pinFallbackHubRuntimes(t)
	t.Setenv(moltenHubTokenEnvVar, "t_env-agent-123")
	t.Setenv(moltenHubRegionEnvVar, HubRegionEU)

	service, fake, store := newServiceForTest(t, func(state *AppState) {
		state.Settings.HubRegion = HubRegionNA
		state.Settings.HubURL = hubURLForRegion(HubRegionNA)
		state.Session.AgentToken = "persisted-token"
		state.Session.BindToken = "persisted-token"
	})
	fake.capabilities = map[string]any{
		"agent_uuid": "agent-1",
		"agent_uri":  "molten://agent/env-agent",
		"handle":     "env-agent",
	}

	if err := service.ConnectFromEnvIfNeeded(context.Background()); err != nil {
		t.Fatalf("connect from env: %v", err)
	}
	if len(fake.bindRequests) != 0 {
		t.Fatalf("bind requests = %d, want 0", len(fake.bindRequests))
	}
	if len(fake.capabilitiesTokens) != 2 {
		t.Fatalf("capability checks = %d, want 2", len(fake.capabilitiesTokens))
	}
	for _, token := range fake.capabilitiesTokens {
		if token != "t_env-agent-123" {
			t.Fatalf("capability token = %q, want t_env-agent-123", token)
		}
	}
	state := store.Snapshot()
	if got := state.Session.AgentToken; got != "t_env-agent-123" {
		t.Fatalf("agent_token = %q, want t_env-agent-123", got)
	}
	if got := state.Settings.HubRegion; got != HubRegionEU {
		t.Fatalf("hub_region = %q, want %q", got, HubRegionEU)
	}
	if got := state.Session.Handle; got != "env-agent" {
		t.Fatalf("handle = %q, want env-agent", got)
	}
}

func TestBindAndRegisterUsesBindPrefixForHandle(t *testing.T) {
	pinFallbackHubRuntimes(t)

	service, fake, store := newServiceForTest(t, nil)
	fake.bindResponse = hub.BindResponse{
		AgentToken: "t_bound",
		AgentUUID:  "agent-1",
		AgentURI:   "molten://agent/new-agent",
		Handle:     "new-agent",
		APIBase:    hubURLForRegion(HubRegionNA),
	}

	err := service.BindAndRegister(context.Background(), BindProfile{
		AgentMode: OnboardingModeExisting,
		BindToken: "b_bind-123",
		Handle:    "new-agent",
	})
	if err != nil {
		t.Fatalf("bind and register: %v", err)
	}
	if len(fake.bindRequests) != 1 {
		t.Fatalf("bind requests = %d, want 1", len(fake.bindRequests))
	}
	if got := fake.bindRequests[0].Handle; got != "new-agent" {
		t.Fatalf("bind handle = %q, want new-agent", got)
	}
	if len(fake.metadataRequests) != 1 {
		t.Fatalf("metadata requests = %d, want 1", len(fake.metadataRequests))
	}
	state := store.Snapshot()
	if got := state.Session.AgentToken; got != "t_bound" {
		t.Fatalf("agent_token = %q, want t_bound", got)
	}
}

func TestBindAndRegisterUsesAgentPrefixAsExisting(t *testing.T) {
	pinFallbackHubRuntimes(t)

	service, fake, store := newServiceForTest(t, nil)
	fake.capabilities = map[string]any{
		"agent_uuid": "agent-1",
		"agent_uri":  "molten://agent/existing-agent",
		"handle":     "existing-agent",
	}

	err := service.BindAndRegister(context.Background(), BindProfile{
		AgentMode: OnboardingModeNew,
		BindToken: "t_existing-123",
		Handle:    "ignored-handle",
	})
	if err != nil {
		t.Fatalf("bind and register: %v", err)
	}
	if len(fake.bindRequests) != 0 {
		t.Fatalf("bind requests = %d, want 0", len(fake.bindRequests))
	}
	if len(fake.metadataRequests) != 0 {
		t.Fatalf("metadata requests = %d, want 0", len(fake.metadataRequests))
	}
	state := store.Snapshot()
	if got := state.Session.AgentToken; got != "t_existing-123" {
		t.Fatalf("agent_token = %q, want t_existing-123", got)
	}
	if got := state.Session.Handle; got != "existing-agent" {
		t.Fatalf("handle = %q, want existing-agent", got)
	}
}

func newServiceForTest(t *testing.T, mutate func(*AppState)) (*Service, *fakeHubClient, *Store) {
	t.Helper()

	settings := DefaultSettings()
	settings.DataDir = t.TempDir()
	path, err := ResolveStorePath(settings.DataDir)
	if err != nil {
		t.Fatalf("resolve store path: %v", err)
	}
	store, err := NewStore(path, settings)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if mutate != nil {
		if err := store.Update(func(state *AppState) error {
			mutate(state)
			return nil
		}); err != nil {
			t.Fatalf("mutate store: %v", err)
		}
	}
	fake := &fakeHubClient{}
	service := NewService(store, fake)
	return service, fake, store
}

func pinFallbackHubRuntimes(t *testing.T) {
	t.Helper()

	hubRuntimeCatalogMu.Lock()
	previousLoaded := hubRuntimeCatalogLoaded
	previousCatalog := cloneHubRuntimes(hubRuntimeCatalog)
	hubRuntimeCatalogLoaded = true
	hubRuntimeCatalog = cloneHubRuntimes(fallbackHubRuntimes)
	hubRuntimeCatalogMu.Unlock()

	t.Cleanup(func() {
		hubRuntimeCatalogMu.Lock()
		hubRuntimeCatalogLoaded = previousLoaded
		hubRuntimeCatalog = previousCatalog
		hubRuntimeCatalogMu.Unlock()
	})
}
