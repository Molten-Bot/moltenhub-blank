package app

import "time"

const (
	ConnectionStatusDisconnected = "disconnected"
	ConnectionStatusConnected    = "connected"

	ConnectionTransportHTTP      = "http"
	ConnectionTransportReachable = "reachable"
	ConnectionTransportRetrying  = "retrying"
	ConnectionTransportHTTPLong  = "http_long_poll"
	ConnectionTransportWebSocket = "ws"
	ConnectionTransportOffline   = "offline"

	OnboardingModeNew      = "new"
	OnboardingModeExisting = "existing"
)

type Settings struct {
	ListenAddr                   string        `json:"listen_addr"`
	HubRegion                    string        `json:"hub_region"`
	HubURL                       string        `json:"hub_url"`
	SessionKey                   string        `json:"session_key"`
	PollInterval                 time.Duration `json:"poll_interval"`
	DataDir                      string        `json:"data_dir"`
	GoogleAnalyticsMeasurementID string        `json:"google_analytics_measurement_id,omitempty"`
}

type Session struct {
	BoundAt         time.Time `json:"bound_at"`
	HubURL          string    `json:"hub_url"`
	APIBase         string    `json:"api_base"`
	AgentToken      string    `json:"agent_token"`
	BindToken       string    `json:"bind_token,omitempty"`
	AgentUUID       string    `json:"agent_uuid"`
	AgentURI        string    `json:"agent_uri"`
	Handle          string    `json:"handle"`
	DisplayName     string    `json:"display_name"`
	Emoji           string    `json:"emoji"`
	ProfileBio      string    `json:"profile_bio"`
	ManifestURL     string    `json:"manifest_url,omitempty"`
	MetadataURL     string    `json:"metadata_url,omitempty"`
	CapabilitiesURL string    `json:"capabilities_url,omitempty"`
	OpenClawPullURL string    `json:"openclaw_pull_url,omitempty"`
	OpenClawPushURL string    `json:"openclaw_push_url,omitempty"`
	OfflineURL      string    `json:"offline_url,omitempty"`
	OfflineMarked   bool      `json:"offline_marked"`
}

type ConnectionState struct {
	Status        string    `json:"status"`
	Transport     string    `json:"transport"`
	LastChangedAt time.Time `json:"last_changed_at"`
	Error         string    `json:"error,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	BaseURL       string    `json:"base_url,omitempty"`
	Domain        string    `json:"domain,omitempty"`
}

type RuntimeEvent struct {
	At     time.Time `json:"at"`
	Level  string    `json:"level"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
}

type FlashMessage struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type AppState struct {
	Settings     Settings        `json:"settings"`
	Session      Session         `json:"session"`
	Connection   ConnectionState `json:"connection"`
	Flash        FlashMessage    `json:"flash"`
	RecentEvents []RuntimeEvent  `json:"recent_events"`
}

type BindProfile struct {
	AgentMode       string
	AgentToken      string
	BindToken       string
	Handle          string
	DisplayName     string
	Emoji           string
	ProfileMarkdown string
}

type AgentProfile struct {
	Handle          string
	DisplayName     string
	Emoji           string
	ProfileMarkdown string
}
