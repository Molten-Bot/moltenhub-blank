package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultDataDir        = ".moltenhub"
	moltenHubRegionEnvVar = "MOLTEN_HUB_REGION"
	moltenHubTokenEnvVar  = "MOLTEN_HUB_TOKEN"
	maxRecentEvents       = 40
)

type Store struct {
	path  string
	mu    sync.RWMutex
	state AppState
}

type persistedConfig struct {
	Settings persistedSettings `json:"settings,omitempty"`
	Session  persistedSession  `json:"session,omitempty"`
}

type persistedSettings struct {
	HubURL    string `json:"hub_url,omitempty"`
	HubRegion string `json:"hub_region,omitempty"`
}

type persistedSession struct {
	AgentToken string `json:"agent_token,omitempty"`
	BindToken  string `json:"bind_token,omitempty"`
}

func DefaultSettings() Settings {
	runtime := defaultRuntimeFromEnv()
	return Settings{
		ListenAddr:   envOrDefault("LISTEN_ADDR", ":8080"),
		HubRegion:    runtime.ID,
		HubURL:       runtime.HubURL,
		SessionKey:   envOrDefault("MOLTEN_HUB_SESSION_KEY", "main"),
		PollInterval: 2 * time.Second,
		DataDir:      envOrDefault("APP_DATA_DIR", defaultDataDir),
	}
}

func ResolveStorePath(dataDir string) (string, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	return filepath.Join(dataDir, "config.json"), nil
}

func NewStore(path string, defaults Settings) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}
	store := &Store{
		path: path,
		state: AppState{
			Settings: defaults,
			Connection: ConnectionState{
				Status:    ConnectionStatusDisconnected,
				Transport: ConnectionTransportOffline,
			},
		},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, store.saveLocked()
		}
		return nil, fmt.Errorf("read store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}
	var persisted persistedConfig
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	applyPersistedConfig(&store.state, persisted)
	mergeDefaultSettings(&store.state.Settings, defaults)
	normalizeState(&store.state)
	return store, nil
}

func (s *Store) Snapshot() AppState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.state)
}

func (s *Store) Update(fn func(*AppState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	normalizeState(&s.state)
	return s.saveLocked()
}

func (s *Store) AppendEvent(event RuntimeEvent) error {
	return s.Update(func(state *AppState) error {
		state.RecentEvents = append([]RuntimeEvent{event}, state.RecentEvents...)
		if len(state.RecentEvents) > maxRecentEvents {
			state.RecentEvents = state.RecentEvents[:maxRecentEvents]
		}
		return nil
	})
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(persistedConfigFromState(s.state), "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	return os.WriteFile(s.path, data, 0o644)
}

func applyPersistedConfig(state *AppState, persisted persistedConfig) {
	state.Settings.HubRegion = persisted.Settings.HubRegion
	state.Settings.HubURL = persisted.Settings.HubURL
	state.Session.AgentToken = persisted.Session.AgentToken
	state.Session.BindToken = persisted.Session.BindToken
}

func persistedConfigFromState(state AppState) persistedConfig {
	return persistedConfig{
		Settings: persistedSettings{
			HubURL:    state.Settings.HubURL,
			HubRegion: state.Settings.HubRegion,
		},
		Session: persistedSession{
			AgentToken: state.Session.AgentToken,
			BindToken:  state.Session.BindToken,
		},
	}
}

func mergeDefaultSettings(settings *Settings, defaults Settings) {
	runtime, err := ResolveHubRuntime(settings.HubRegion, settings.HubURL)
	if envRuntime, envErr, ok := runtimeFromEnv(); ok && envErr == nil {
		runtime = envRuntime
		err = nil
	}
	if err != nil {
		runtime, err = ResolveHubRuntime(defaults.HubRegion, defaults.HubURL)
		if err != nil {
			runtime = DefaultHubRuntime()
		}
	}
	settings.HubRegion = runtime.ID
	settings.HubURL = runtime.HubURL
	if settings.ListenAddr == "" {
		settings.ListenAddr = defaults.ListenAddr
	}
	if settings.SessionKey == "" {
		settings.SessionKey = defaults.SessionKey
	}
	if settings.PollInterval == 0 {
		settings.PollInterval = defaults.PollInterval
	}
	if value, ok := envValue("APP_DATA_DIR"); ok {
		settings.DataDir = value
	} else if settings.DataDir == "" {
		settings.DataDir = defaults.DataDir
	}
}

func normalizeState(state *AppState) {
	if state == nil {
		return
	}
	runtime, err := ResolveHubRuntime(state.Settings.HubRegion, state.Settings.HubURL)
	if err != nil {
		runtime = DefaultHubRuntime()
	}
	state.Settings.HubRegion = runtime.ID
	state.Settings.HubURL = runtime.HubURL
	state.Session.AgentToken = coalesceTrimmed(state.Session.AgentToken, state.Session.BindToken)
	state.Session.BindToken = coalesceTrimmed(state.Session.BindToken, state.Session.AgentToken)
	state.Session.HubURL = normalizeHubRuntimeURL(state.Session.HubURL)
	state.Session.APIBase = NormalizeHubEndpointURL(state.Session.APIBase)
	if state.Session.APIBase == "" {
		state.Session.APIBase = NormalizeHubEndpointURL(state.Settings.HubURL)
	}
	if state.Connection.Status == "" {
		state.Connection.Status = ConnectionStatusDisconnected
	}
	if state.Connection.Transport == "" {
		state.Connection.Transport = ConnectionTransportOffline
	}
	state.Connection.BaseURL, state.Connection.Domain = hubConnectionTarget(state.Session.APIBase, state.Settings.HubURL)
	state.Flash.Level = strings.ToLower(strings.TrimSpace(state.Flash.Level))
	state.Flash.Message = strings.TrimSpace(state.Flash.Message)
}

func cloneState(state AppState) AppState {
	state.RecentEvents = append([]RuntimeEvent(nil), state.RecentEvents...)
	return state
}

func defaultRuntimeFromEnv() HubRuntime {
	runtime, err, ok := runtimeFromEnv()
	if ok && err == nil {
		return runtime
	}
	return DefaultHubRuntime()
}

func runtimeFromEnv() (HubRuntime, error, bool) {
	region, ok := envValue(moltenHubRegionEnvVar)
	if !ok {
		return HubRuntime{}, nil, false
	}
	runtime, err := ResolveHubRuntime(region, "")
	return runtime, err, true
}

func envValue(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(normalizeEnvAssignmentValue(value)), true
}

func envOrDefault(name, fallback string) string {
	if value, ok := envValue(name); ok && value != "" {
		return value
	}
	return fallback
}

func normalizeEnvAssignmentValue(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, ":"); idx > 0 && !strings.Contains(value[:idx], "/") {
		return strings.TrimSpace(value[idx+1:])
	}
	return value
}

func NewID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(buf))
}

func coalesceTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hubConnectionTarget(baseURL, hubURL string) (string, string) {
	baseURL = NormalizeHubEndpointURL(coalesceTrimmed(baseURL, hubURL))
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL, ""
	}
	return baseURL, parsed.Hostname()
}
