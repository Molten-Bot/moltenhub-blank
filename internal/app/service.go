package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moltenbot000/moltenhub-blank/internal/hub"
)

const (
	blankHarness          = "moltenhub-blank"
	hubPingRetryInterval  = 12 * time.Second
	hubPingRequestTimeout = 6 * time.Second
	wsFallbackWindow      = 30 * time.Second
	wsUpgradeRetryWindow  = 5 * time.Second
)

type HubClient interface {
	BindAgent(ctx context.Context, req hub.BindRequest) (hub.BindResponse, error)
	UpdateMetadata(ctx context.Context, token string, req hub.UpdateMetadataRequest) (map[string]any, error)
	GetCapabilities(ctx context.Context, token string) (map[string]any, error)
	PullOpenClaw(ctx context.Context, token string, timeout time.Duration) (hub.PullResponse, bool, error)
	AckOpenClaw(ctx context.Context, token, deliveryID string) error
	NackOpenClaw(ctx context.Context, token, deliveryID string) error
	MarkOffline(ctx context.Context, token string, req hub.OfflineRequest) error
}

type realtimeHubClient interface {
	ConnectOpenClaw(ctx context.Context, token, sessionKey string) (hub.RealtimeSession, error)
}

type hubPingClient interface {
	CheckPing(ctx context.Context) (string, error)
}

type baseURLSetter interface {
	SetBaseURL(baseURL string)
}

type runtimeEndpointSetter interface {
	SetRuntimeEndpoints(endpoints hub.RuntimeEndpoints)
}

type Service struct {
	store               *Store
	hub                 HubClient
	hubPingRetryDelay   time.Duration
	hubPingCheckTimeout time.Duration
	wsFallbackWindow    time.Duration
	wsUpgradeRetryDelay time.Duration
	presenceSynced      bool
	presenceTransport   string
}

func NewService(store *Store, hubClient HubClient) *Service {
	service := &Service{
		store:               store,
		hub:                 hubClient,
		hubPingRetryDelay:   hubPingRetryInterval,
		hubPingCheckTimeout: hubPingRequestTimeout,
		wsFallbackWindow:    wsFallbackWindow,
		wsUpgradeRetryDelay: wsUpgradeRetryWindow,
	}
	service.configureHubClient(store.Snapshot())
	return service
}

func (s *Service) Snapshot() AppState {
	return s.store.Snapshot()
}

func (s *Service) SetFlash(level, message string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	message = strings.TrimSpace(message)
	return s.store.Update(func(state *AppState) error {
		state.Flash = FlashMessage{Level: level, Message: message}
		return nil
	})
}

func (s *Service) ConsumeFlash() (FlashMessage, error) {
	state := s.store.Snapshot()
	if strings.TrimSpace(state.Flash.Message) == "" {
		return FlashMessage{}, nil
	}
	var flash FlashMessage
	err := s.store.Update(func(state *AppState) error {
		flash = state.Flash
		state.Flash = FlashMessage{}
		return nil
	})
	return flash, err
}

func (s *Service) UpdateSettings(mutator func(*Settings) error) error {
	err := s.store.Update(func(state *AppState) error {
		if err := mutator(&state.Settings); err != nil {
			return err
		}
		runtime, err := ResolveHubRuntime(state.Settings.HubRegion, state.Settings.HubURL)
		if err != nil {
			return err
		}
		state.Settings.HubRegion = runtime.ID
		state.Settings.HubURL = runtime.HubURL
		return nil
	})
	if err != nil {
		return err
	}
	s.configureHubClient(s.store.Snapshot())
	return nil
}

func (s *Service) BindAndRegister(ctx context.Context, profile BindProfile) error {
	state := s.store.Snapshot()
	runtime, err := ResolveHubRuntime(state.Settings.HubRegion, state.Settings.HubURL)
	if err != nil {
		return WrapOnboardingError(OnboardingStepBind, err)
	}
	mode, bindToken, agentToken := NormalizeOnboardingTokens(profile.AgentMode, profile.BindToken, profile.AgentToken)
	if mode == OnboardingModeExisting {
		return s.connectExistingAgent(ctx, runtime, agentToken, profile)
	}
	s.setHubBaseURL(runtime.HubURL)
	result, err := s.hub.BindAgent(ctx, hub.BindRequest{
		HubURL:    runtime.HubURL,
		BindToken: bindToken,
		Handle:    strings.TrimSpace(profile.Handle),
	})
	if err != nil {
		return WrapOnboardingError(OnboardingStepBind, err)
	}
	result.AgentToken = strings.TrimSpace(result.AgentToken)
	if result.AgentToken == "" {
		return WrapOnboardingError(OnboardingStepBind, errors.New("bind response missing agent token"))
	}
	runtimeEndpoints := runtimeEndpointsFromBind(result)
	apiBase := NormalizeHubEndpointURL(coalesceTrimmed(result.APIBase, runtime.HubURL))
	if apiBase == "" {
		return WrapOnboardingError(OnboardingStepBind, errors.New("bind response missing supported api_base"))
	}
	s.setHubBaseURL(apiBase)
	s.setRuntimeEndpoints(runtimeEndpoints)
	session := Session{
		BoundAt:         time.Now().UTC(),
		HubURL:          runtime.HubURL,
		APIBase:         apiBase,
		AgentToken:      result.AgentToken,
		BindToken:       result.AgentToken,
		AgentUUID:       result.AgentUUID,
		AgentURI:        result.AgentURI,
		Handle:          coalesceTrimmed(result.Handle, profile.Handle),
		DisplayName:     coalesceTrimmed(profile.DisplayName, "Molten Hub Blank"),
		Emoji:           coalesceTrimmed(profile.Emoji, "*"),
		ProfileBio:      coalesceTrimmed(profile.ProfileMarkdown, "Blank Molten Hub connectivity app."),
		ManifestURL:     runtimeEndpoints.ManifestURL,
		MetadataURL:     runtimeEndpoints.MetadataURL,
		CapabilitiesURL: runtimeEndpoints.CapabilitiesURL,
		OpenClawPullURL: runtimeEndpoints.OpenClawPullURL,
		OpenClawPushURL: runtimeEndpoints.OpenClawPushURL,
		OfflineURL:      runtimeEndpoints.OpenClawOfflineURL,
	}
	if err := s.storeConnectedSession(runtime, session); err != nil {
		return WrapOnboardingError(OnboardingStepBind, err)
	}
	if _, err := s.hub.GetCapabilities(ctx, result.AgentToken); err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return WrapOnboardingError(OnboardingStepWorkBind, fmt.Errorf("agent bound, but credential verification failed: %w", err))
	}
	s.noteHubInteraction(nil, ConnectionTransportHTTP)
	if err := s.updateAgentProfile(ctx, result.AgentToken, AgentProfile{DisplayName: session.DisplayName, Emoji: session.Emoji, ProfileMarkdown: session.ProfileBio}); err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return WrapOnboardingError(OnboardingStepProfileSet, fmt.Errorf("agent bound, but profile registration failed: %w", err))
	}
	if _, err := s.hub.GetCapabilities(ctx, result.AgentToken); err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return WrapOnboardingError(OnboardingStepWorkActivate, fmt.Errorf("agent bound and profile registered, but activation check failed: %w", err))
	}
	s.noteHubInteraction(nil, ConnectionTransportHTTP)
	_ = s.logEvent("info", "Agent bound", "Blank app credential verified against "+apiBase)
	return nil
}

func (s *Service) connectExistingAgent(ctx context.Context, runtime HubRuntime, agentToken string, profile BindProfile) error {
	agentToken = strings.TrimSpace(agentToken)
	if agentToken == "" {
		return WrapOnboardingError(OnboardingStepBind, errors.New("agent token is required"))
	}
	apiBase := NormalizeHubEndpointURL(defaultAPIBaseForHub(runtime.HubURL))
	if apiBase == "" {
		return WrapOnboardingError(OnboardingStepBind, fmt.Errorf("runtime config missing supported api_base for %q", runtime.HubURL))
	}
	s.setHubBaseURL(apiBase)
	s.setRuntimeEndpoints(hub.RuntimeEndpoints{})
	capabilities, err := s.hub.GetCapabilities(ctx, agentToken)
	if err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return WrapOnboardingError(OnboardingStepWorkBind, fmt.Errorf("existing agent credential verification failed: %w", err))
	}
	s.noteHubInteraction(nil, ConnectionTransportHTTP)
	identity := identityFromCapabilities(capabilities)
	session := Session{
		BoundAt:     time.Now().UTC(),
		HubURL:      runtime.HubURL,
		APIBase:     apiBase,
		AgentToken:  agentToken,
		BindToken:   agentToken,
		AgentUUID:   identity.AgentUUID,
		AgentURI:    identity.AgentURI,
		Handle:      identity.Handle,
		DisplayName: coalesceTrimmed(profile.DisplayName, identity.DisplayName, "Molten Hub Blank"),
		Emoji:       coalesceTrimmed(profile.Emoji, identity.Emoji, "*"),
		ProfileBio:  coalesceTrimmed(profile.ProfileMarkdown, identity.ProfileMarkdown, "Blank Molten Hub connectivity app."),
	}
	if err := s.storeConnectedSession(runtime, session); err != nil {
		return WrapOnboardingError(OnboardingStepBind, err)
	}
	if err := s.updateAgentProfile(ctx, agentToken, AgentProfile{DisplayName: session.DisplayName, Emoji: session.Emoji, ProfileMarkdown: session.ProfileBio}); err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return WrapOnboardingError(OnboardingStepProfileSet, fmt.Errorf("existing agent verified, but profile registration failed: %w", err))
	}
	_ = s.logEvent("info", "Existing agent connected", "Blank app credential verified against "+apiBase)
	return nil
}

func (s *Service) DisconnectAgent(ctx context.Context) error {
	_ = s.MarkOffline(ctx, "manual disconnect")
	s.presenceSynced = false
	s.presenceTransport = ""
	return s.store.Update(func(state *AppState) error {
		state.Session = Session{}
		state.Connection = ConnectionState{Status: ConnectionStatusDisconnected, Transport: ConnectionTransportOffline, LastChangedAt: time.Now().UTC()}
		return nil
	})
}

func (s *Service) ConnectFromEnvIfNeeded(ctx context.Context) error {
	token, ok := envValue(moltenHubTokenEnvVar)
	if !ok || token == "" {
		return nil
	}
	runtime, err, configured := runtimeFromEnv()
	if !configured {
		err = fmt.Errorf("%s is required when %s is set", moltenHubRegionEnvVar, moltenHubTokenEnvVar)
		_ = s.SetFlash("error", err.Error())
		return err
	}
	if err != nil {
		err = fmt.Errorf("automatic hub connection from %s failed: %w", moltenHubTokenEnvVar, err)
		_ = s.SetFlash("error", err.Error())
		return err
	}
	if err := s.UpdateSettings(func(settings *Settings) error {
		settings.HubRegion = runtime.ID
		settings.HubURL = runtime.HubURL
		return nil
	}); err != nil {
		return err
	}
	mode, bindToken, agentToken := NormalizeOnboardingTokens("", token, "")
	err = s.BindAndRegister(ctx, BindProfile{AgentMode: mode, BindToken: bindToken, AgentToken: agentToken})
	if err != nil {
		err = fmt.Errorf("automatic hub connection from %s failed: %w", moltenHubTokenEnvVar, err)
		_ = s.SetFlash("error", err.Error())
		return err
	}
	_ = s.SetFlash("info", "Hub connected from "+moltenHubTokenEnvVar+".")
	return nil
}

func (s *Service) RunHubLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		state := s.store.Snapshot()
		if strings.TrimSpace(state.Session.AgentToken) == "" {
			if !sleepWithContext(ctx, state.Settings.PollInterval) {
				return
			}
			continue
		}
		s.configureHubClient(state)
		if err := s.waitForHubReachable(ctx); err != nil {
			return
		}
		state = s.store.Snapshot()
		if realtime, ok := s.hub.(realtimeHubClient); ok {
			fallback, err := s.runRealtimeCycle(ctx, realtime, state.Session.AgentToken, state.Settings.SessionKey)
			if err == nil || ctx.Err() != nil {
				continue
			}
			if isUnauthorizedHubError(err) || !fallback {
				return
			}
			s.noteRealtimeFallback(err)
			_ = s.runHTTPFallbackWindow(ctx, realtime)
			continue
		}
		if err := s.pollOnce(ctx); err != nil && isUnauthorizedHubError(err) {
			return
		}
		if !sleepWithContext(ctx, state.Settings.PollInterval) {
			return
		}
	}
}

func (s *Service) pollOnce(ctx context.Context) error {
	state := s.store.Snapshot()
	if state.Session.AgentToken == "" {
		return nil
	}
	s.configureHubClient(state)
	if err := s.ensurePresenceOnline(ctx, ConnectionTransportHTTPLong); err != nil {
		return err
	}
	message, ok, err := s.hub.PullOpenClaw(ctx, state.Session.AgentToken, 25*time.Second)
	if err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTPLong)
		return err
	}
	s.noteHubInteraction(nil, ConnectionTransportHTTPLong)
	if ok {
		_ = s.logEvent("info", "Message ignored", "Blank app received message "+message.MessageID)
		return s.hub.AckOpenClaw(ctx, state.Session.AgentToken, message.DeliveryID)
	}
	return nil
}

func (s *Service) runRealtimeCycle(ctx context.Context, realtime realtimeHubClient, token, sessionKey string) (bool, error) {
	session, err := realtime.ConnectOpenClaw(ctx, token, sessionKey)
	if err != nil {
		return true, err
	}
	defer session.Close()
	if err := s.ensurePresenceOnline(ctx, ConnectionTransportWebSocket); err != nil {
		return true, err
	}
	s.noteHubInteraction(nil, ConnectionTransportWebSocket)
	for {
		message, err := session.Receive(ctx)
		if err != nil {
			if shouldFallbackToLongPoll(err) {
				return true, err
			}
			return false, err
		}
		_ = s.logEvent("info", "Message ignored", "Blank app received realtime message "+message.MessageID)
		_ = session.Ack(ctx, message.DeliveryID)
	}
}

func (s *Service) runHTTPFallbackWindow(ctx context.Context, realtime realtimeHubClient) error {
	deadline := time.Now().Add(s.wsFallbackWindow)
	nextWSAttempt := time.Now().Add(s.wsUpgradeRetryDelay)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.pollOnce(ctx); err != nil && isUnauthorizedHubError(err) {
			return err
		}
		if time.Now().After(nextWSAttempt) {
			state := s.store.Snapshot()
			fallback, err := s.runRealtimeCycle(ctx, realtime, state.Session.AgentToken, state.Settings.SessionKey)
			if err == nil || !fallback || isUnauthorizedHubError(err) {
				return err
			}
			s.noteRealtimeFallback(err)
			nextWSAttempt = time.Now().Add(s.wsUpgradeRetryDelay)
		}
		if time.Now().After(deadline) {
			return nil
		}
		if !sleepWithContext(ctx, s.store.Snapshot().Settings.PollInterval) {
			return ctx.Err()
		}
	}
}

func (s *Service) waitForHubReachable(ctx context.Context) error {
	pinger, ok := s.hub.(hubPingClient)
	if !ok {
		return nil
	}
	for {
		timeoutCtx, cancel := context.WithTimeout(ctx, s.hubPingCheckTimeout)
		detail, err := pinger.CheckPing(timeoutCtx)
		cancel()
		if err == nil {
			s.noteHubPingReachable(detail)
			return nil
		}
		s.noteHubPingRetrying(err, s.hubPingRetryDelay)
		if !sleepWithContext(ctx, s.hubPingRetryDelay) {
			return ctx.Err()
		}
	}
}

func (s *Service) MarkOffline(ctx context.Context, reason string) error {
	state := s.store.Snapshot()
	if state.Session.AgentToken == "" || state.Session.OfflineMarked {
		return nil
	}
	s.configureHubClient(state)
	if err := s.hub.MarkOffline(ctx, state.Session.AgentToken, hub.OfflineRequest{SessionKey: state.Settings.SessionKey, Reason: reason}); err != nil {
		s.noteHubInteraction(err, ConnectionTransportHTTP)
		return err
	}
	s.presenceSynced = false
	s.presenceTransport = ""
	return s.store.Update(func(state *AppState) error {
		state.Session.OfflineMarked = true
		state.Connection = ConnectionState{Status: ConnectionStatusDisconnected, Transport: ConnectionTransportOffline, LastChangedAt: time.Now().UTC(), Detail: reason}
		return nil
	})
}

func (s *Service) MarkOnline(ctx context.Context, transport string) error {
	state := s.store.Snapshot()
	if state.Session.AgentToken == "" {
		return nil
	}
	s.configureHubClient(state)
	_, err := s.hub.UpdateMetadata(ctx, state.Session.AgentToken, hub.UpdateMetadataRequest{
		Metadata: buildAgentMetadata(state.Session, state.Settings.SessionKey, normalizePresenceTransport(transport)),
	})
	if err != nil {
		s.noteHubInteraction(err, transport)
		return err
	}
	s.presenceSynced = true
	s.presenceTransport = normalizePresenceTransport(transport)
	s.noteHubInteraction(nil, transport)
	return nil
}

func (s *Service) ensurePresenceOnline(ctx context.Context, transport string) error {
	transport = normalizePresenceTransport(transport)
	if s.presenceSynced && s.presenceTransport == transport {
		return nil
	}
	return s.MarkOnline(ctx, transport)
}

func (s *Service) updateAgentProfile(ctx context.Context, token string, profile AgentProfile) error {
	_, err := s.hub.UpdateMetadata(ctx, token, hub.UpdateMetadataRequest{
		Handle: strings.TrimSpace(profile.Handle),
		Metadata: map[string]any{
			"harness":          blankHarness,
			"display_name":     coalesceTrimmed(profile.DisplayName, "Molten Hub Blank"),
			"emoji":            coalesceTrimmed(profile.Emoji, "*"),
			"profile_markdown": coalesceTrimmed(profile.ProfileMarkdown, "Blank Molten Hub connectivity app."),
			"capabilities":     []string{},
			"skills":           []map[string]string{},
		},
	})
	return err
}

func (s *Service) configureHubClient(state AppState) {
	baseURL := NormalizeHubEndpointURL(coalesceTrimmed(state.Session.APIBase, state.Settings.HubURL))
	s.setHubBaseURL(baseURL)
	s.setRuntimeEndpoints(runtimeEndpointsFromSession(state.Session))
}

func (s *Service) setHubBaseURL(baseURL string) {
	if setter, ok := s.hub.(baseURLSetter); ok && NormalizeHubEndpointURL(baseURL) != "" {
		setter.SetBaseURL(baseURL)
	}
}

func (s *Service) setRuntimeEndpoints(endpoints hub.RuntimeEndpoints) {
	if setter, ok := s.hub.(runtimeEndpointSetter); ok {
		setter.SetRuntimeEndpoints(endpoints)
	}
}

func (s *Service) storeConnectedSession(runtime HubRuntime, session Session) error {
	now := time.Now().UTC()
	return s.store.Update(func(state *AppState) error {
		state.Settings.HubRegion = runtime.ID
		state.Settings.HubURL = runtime.HubURL
		state.Session = session
		state.Session.OfflineMarked = false
		state.Connection = ConnectionState{Status: ConnectionStatusConnected, Transport: ConnectionTransportHTTP, LastChangedAt: now, BaseURL: session.APIBase}
		return nil
	})
}

func (s *Service) noteHubInteraction(err error, transport string) {
	now := time.Now().UTC()
	_ = s.store.Update(func(state *AppState) error {
		state.Connection.Status = ConnectionStatusConnected
		state.Connection.Transport = normalizePresenceTransport(transport)
		state.Connection.LastChangedAt = now
		state.Connection.Error = ""
		state.Connection.Detail = ""
		if err != nil {
			state.Connection.Status = ConnectionStatusDisconnected
			state.Connection.Error = strings.TrimSpace(err.Error())
		}
		return nil
	})
}

func (s *Service) noteHubPingRetrying(err error, retryDelay time.Duration) {
	_ = s.store.Update(func(state *AppState) error {
		state.Connection = ConnectionState{Status: ConnectionStatusDisconnected, Transport: ConnectionTransportRetrying, LastChangedAt: time.Now().UTC(), Error: err.Error(), Detail: fmt.Sprintf("Retrying in %s.", retryDelay)}
		return nil
	})
}

func (s *Service) noteHubPingReachable(detail string) {
	_ = s.store.Update(func(state *AppState) error {
		if state.Connection.Status != ConnectionStatusConnected {
			state.Connection = ConnectionState{Status: ConnectionStatusDisconnected, Transport: ConnectionTransportReachable, LastChangedAt: time.Now().UTC(), Detail: detail}
		}
		state.Session.OfflineMarked = false
		return nil
	})
}

func (s *Service) noteRealtimeFallback(err error) {
	_ = s.store.Update(func(state *AppState) error {
		state.Connection = ConnectionState{Status: ConnectionStatusDisconnected, Transport: ConnectionTransportReachable, LastChangedAt: time.Now().UTC(), Error: err.Error(), Detail: "WebSocket unavailable; falling back to HTTP long polling."}
		return nil
	})
}

func (s *Service) logEvent(level, title, detail string) error {
	return s.store.AppendEvent(RuntimeEvent{At: time.Now().UTC(), Level: strings.TrimSpace(level), Title: strings.TrimSpace(title), Detail: strings.TrimSpace(detail)})
}

func normalizePresenceTransport(transport string) string {
	switch strings.TrimSpace(transport) {
	case ConnectionTransportWebSocket:
		return ConnectionTransportWebSocket
	case ConnectionTransportHTTPLong:
		return ConnectionTransportHTTPLong
	case ConnectionTransportReachable:
		return ConnectionTransportReachable
	case ConnectionTransportRetrying:
		return ConnectionTransportRetrying
	case ConnectionTransportHTTP:
		return ConnectionTransportHTTP
	default:
		return ConnectionTransportOffline
	}
}

func buildAgentMetadata(session Session, sessionKey, transport string) map[string]any {
	return map[string]any{
		"harness":          blankHarness,
		"display_name":     coalesceTrimmed(session.DisplayName, "Molten Hub Blank"),
		"emoji":            coalesceTrimmed(session.Emoji, "*"),
		"profile_markdown": coalesceTrimmed(session.ProfileBio, "Blank Molten Hub connectivity app."),
		"session_key":      coalesceTrimmed(sessionKey, "main"),
		"transport":        normalizePresenceTransport(transport),
		"capabilities":     []string{},
		"skills":           []map[string]string{},
	}
}

func runtimeEndpointsFromBind(result hub.BindResponse) hub.RuntimeEndpoints {
	return hub.RuntimeEndpoints{
		ManifestURL:        result.Endpoints.Manifest,
		CapabilitiesURL:    result.Endpoints.Capabilities,
		MetadataURL:        result.Endpoints.Metadata,
		OpenClawPullURL:    result.Endpoints.OpenClawPull,
		OpenClawPushURL:    result.Endpoints.OpenClawPush,
		OpenClawOfflineURL: result.Endpoints.Offline,
	}
}

func runtimeEndpointsFromSession(session Session) hub.RuntimeEndpoints {
	return hub.RuntimeEndpoints{
		ManifestURL:        session.ManifestURL,
		CapabilitiesURL:    session.CapabilitiesURL,
		MetadataURL:        session.MetadataURL,
		OpenClawPullURL:    session.OpenClawPullURL,
		OpenClawPushURL:    session.OpenClawPushURL,
		OpenClawOfflineURL: session.OfflineURL,
	}
}

type capabilityIdentity struct {
	AgentUUID       string
	AgentURI        string
	Handle          string
	DisplayName     string
	Emoji           string
	ProfileMarkdown string
}

func identityFromCapabilities(capabilities map[string]any) capabilityIdentity {
	var identity capabilityIdentity
	for _, key := range []string{"agent_uuid", "uuid", "id"} {
		identity.AgentUUID = coalesceTrimmed(identity.AgentUUID, stringFromMap(capabilities, key))
	}
	for _, key := range []string{"agent_uri", "uri"} {
		identity.AgentURI = coalesceTrimmed(identity.AgentURI, stringFromMap(capabilities, key))
	}
	identity.Handle = stringFromMap(capabilities, "handle")
	if metadata, ok := capabilities["metadata"].(map[string]any); ok {
		identity.DisplayName = stringFromMap(metadata, "display_name")
		identity.Emoji = stringFromMap(metadata, "emoji")
		identity.ProfileMarkdown = stringFromMap(metadata, "profile_markdown")
	}
	return identity
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func isUnauthorizedHubError(err error) bool {
	var apiErr *hub.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}

func shouldFallbackToLongPoll(err error) bool {
	if err == nil || isUnauthorizedHubError(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "websocket") || strings.Contains(message, "connection") || strings.Contains(message, "timeout")
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		delay = 2 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
