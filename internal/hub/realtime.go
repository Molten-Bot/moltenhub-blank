package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

var (
	websocketHeartbeatInterval = 15 * time.Second
	websocketDialTimeout       = 8 * time.Second
	websocketHandshakeTimeout  = 8 * time.Second
)

type RealtimeSession interface {
	Receive(ctx context.Context) (PullResponse, error)
	Ack(ctx context.Context, deliveryID string) error
	Nack(ctx context.Context, deliveryID string) error
	Close() error
}

type websocketSession struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	queue     []PullResponse
	closeOnce sync.Once
	closed    chan struct{}
}

type realtimeEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result"`
	Error     struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) ConnectOpenClaw(ctx context.Context, token, sessionKey string) (RealtimeSession, error) {
	wsURL, err := websocketURL(c.baseURL, c.openClawWebsocketEndpoint(), sessionKey)
	if err != nil {
		return nil, err
	}
	config, err := websocket.NewConfig(wsURL, httpOriginFor(wsURL))
	if err != nil {
		return nil, fmt.Errorf("create websocket config: %w", err)
	}
	config.Dialer = &net.Dialer{Timeout: boundedTimeout(ctx, websocketDialTimeout)}
	config.Header = http.Header{
		"Authorization": []string{"Bearer " + strings.TrimSpace(token)},
		"User-Agent":    []string{c.userAgent},
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		return nil, fmt.Errorf("open websocket session: %w", err)
	}
	session := &websocketSession{conn: conn, closed: make(chan struct{})}
	handshakeCtx, cancelHandshake := context.WithTimeout(ctx, boundedTimeout(ctx, websocketHandshakeTimeout))
	defer cancelHandshake()
	first, err := session.readEnvelope(handshakeCtx)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	if !strings.EqualFold(first.Type, "session_ready") {
		_ = session.Close()
		return nil, fmt.Errorf("unexpected websocket handshake message type %q", first.Type)
	}
	session.bindContext(ctx)
	session.startHeartbeat(ctx)
	return session, nil
}

func (c *Client) openClawWebsocketEndpoint() string {
	if endpoint := strings.TrimSpace(c.endpoints.OpenClawWebsocketURL); endpoint != "" {
		return endpoint
	}
	pullEndpoint := c.runtimeEndpoint(c.endpoints.OpenClawPullURL, "/v1/runtime/messages/pull")
	if endpoint := websocketEndpointFromPull(pullEndpoint); endpoint != "" {
		return endpoint
	}
	return "/v1/runtime/messages/ws"
}

func (s *websocketSession) Receive(ctx context.Context) (PullResponse, error) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		message := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return message, nil
	}
	s.mu.Unlock()
	for {
		envelope, err := s.readEnvelope(ctx)
		if err != nil {
			return PullResponse{}, err
		}
		switch {
		case strings.EqualFold(envelope.Type, "delivery"):
			return decodePullResponsePayload(envelope.Result, "realtime delivery")
		case strings.EqualFold(envelope.Type, "__close__"):
			return PullResponse{}, fmt.Errorf("hub websocket session closed")
		case strings.EqualFold(envelope.Type, "__error__"):
			return PullResponse{}, fmt.Errorf("hub websocket error: %s", envelope.Error.Message)
		}
	}
}

func (s *websocketSession) Ack(ctx context.Context, deliveryID string) error {
	return s.respond(ctx, "ack", deliveryID)
}

func (s *websocketSession) Nack(ctx context.Context, deliveryID string) error {
	return s.respond(ctx, "nack", deliveryID)
}

func (s *websocketSession) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.closed)
		closeErr = s.conn.Close()
	})
	return closeErr
}

func (s *websocketSession) respond(ctx context.Context, action, deliveryID string) error {
	if strings.TrimSpace(deliveryID) == "" {
		return nil
	}
	requestID := action + ":" + deliveryID
	if err := s.writeEnvelope(ctx, map[string]any{"type": action, "request_id": requestID, "delivery_id": deliveryID}); err != nil {
		return err
	}
	for {
		envelope, err := s.readEnvelope(ctx)
		if err != nil {
			return err
		}
		switch {
		case strings.EqualFold(envelope.Type, "delivery"):
			message, decodeErr := decodePullResponsePayload(envelope.Result, "realtime delivery")
			if decodeErr != nil {
				return decodeErr
			}
			s.mu.Lock()
			s.queue = append(s.queue, message)
			s.mu.Unlock()
		case strings.EqualFold(envelope.Type, "response") && envelope.RequestID == requestID:
			if envelope.OK {
				return nil
			}
			return fmt.Errorf("hub websocket %s failed: %s", action, envelope.Error.Message)
		case strings.EqualFold(envelope.Type, "__close__"):
			return fmt.Errorf("hub websocket session closed")
		case strings.EqualFold(envelope.Type, "__error__"):
			return fmt.Errorf("hub websocket error: %s", envelope.Error.Message)
		}
	}
}

func (s *websocketSession) writeEnvelope(ctx context.Context, payload any) error {
	if err := s.applyDeadline(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := websocket.JSON.Send(s.conn, payload); err != nil {
		return fmt.Errorf("send websocket payload: %w", err)
	}
	return nil
}

func (s *websocketSession) readEnvelope(ctx context.Context) (realtimeEnvelope, error) {
	if err := s.applyDeadline(ctx); err != nil {
		return realtimeEnvelope{}, err
	}
	var envelope realtimeEnvelope
	if err := websocket.JSON.Receive(s.conn, &envelope); err != nil {
		return realtimeEnvelope{}, fmt.Errorf("receive websocket payload: %w", err)
	}
	return envelope, nil
}

func (s *websocketSession) applyDeadline(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		return s.conn.SetDeadline(deadline)
	}
	return s.conn.SetDeadline(time.Time{})
}

func (s *websocketSession) bindContext(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.closed:
		}
	}()
}

func (s *websocketSession) startHeartbeat(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(websocketHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.closed:
				return
			case <-ticker.C:
				if err := s.writePing(ctx, []byte("hb")); err != nil {
					_ = s.Close()
					return
				}
			}
		}
	}()
}

func (s *websocketSession) writePing(ctx context.Context, payload []byte) error {
	if err := s.applyDeadline(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := websocket.Message.Send(s.conn, payload); err != nil {
		return fmt.Errorf("send websocket heartbeat: %w", err)
	}
	return nil
}

func boundedTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < fallback {
			return remaining
		}
	}
	return fallback
}

func websocketURL(baseURL, endpoint, sessionKey string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid hub websocket base URL %q", baseURL)
	}
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		u, err = url.Parse(endpoint)
		if err != nil {
			return "", err
		}
	} else {
		u.Path = joinURLPath(u.Path, endpoint)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	values := u.Query()
	if strings.TrimSpace(sessionKey) != "" {
		values.Set("session_key", strings.TrimSpace(sessionKey))
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}

func websocketEndpointFromPull(pullURL string) string {
	pullURL = strings.TrimSpace(pullURL)
	if pullURL == "" {
		return ""
	}
	parsed, err := url.Parse(pullURL)
	if err != nil {
		return ""
	}
	trimmedPath := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(trimmedPath, "/messages/pull"):
		parsed.Path = strings.TrimSuffix(trimmedPath, "/messages/pull") + "/messages/ws"
	case strings.HasSuffix(trimmedPath, "/messages_pull"):
		parsed.Path = strings.TrimSuffix(trimmedPath, "/messages_pull") + "/messages_ws"
	case strings.HasSuffix(trimmedPath, "/pull"):
		parsed.Path = strings.TrimSuffix(trimmedPath, "/pull") + "/ws"
	default:
		return ""
	}
	return parsed.String()
}

func httpOriginFor(wsURL string) string {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return "https://localhost"
	}
	scheme := "https"
	if parsed.Scheme == "ws" {
		scheme = "http"
	}
	return scheme + "://" + parsed.Host
}
