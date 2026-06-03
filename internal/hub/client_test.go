package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseBindResponsePayload(t *testing.T) {
	payload := json.RawMessage(`{
		"agent_token":"t_123",
		"agent_uuid":"agent-1",
		"api_base":"https://na.hub.molten.bot",
		"endpoints":{"messages_pull":"/pull","messages_publish":"/push"}
	}`)
	out, err := parseBindResponsePayload(payload)
	if err != nil {
		t.Fatalf("parseBindResponsePayload() error = %v", err)
	}
	if out.AgentToken != "t_123" || out.Endpoints.OpenClawPull != "/pull" || out.Endpoints.OpenClawPush != "/push" {
		t.Fatalf("unexpected bind response: %#v", out)
	}
}

func TestParseBindResponsePayloadAcceptsTokenAlias(t *testing.T) {
	payload := json.RawMessage(`{
		"token":"t_123",
		"agent_uuid":"agent-1",
		"api_base":"https://na.hub.molten.bot",
		"endpoints":{"messages_pull":"/pull","messages_publish":"/push"}
	}`)
	out, err := parseBindResponsePayload(payload)
	if err != nil {
		t.Fatalf("parseBindResponsePayload() error = %v", err)
	}
	if out.AgentToken != "t_123" || out.Endpoints.OpenClawPull != "/pull" || out.Endpoints.OpenClawPush != "/push" {
		t.Fatalf("unexpected bind response: %#v", out)
	}
}

func TestParseBindResponsePayloadAcceptsNestedTokenAlias(t *testing.T) {
	payload := json.RawMessage(`{
		"agent":{
			"token":"t_123",
			"agent_uuid":"agent-1",
			"api_base":"https://na.hub.molten.bot",
			"endpoints":{"messages_pull":"/pull","messages_publish":"/push"}
		}
	}`)
	out, err := parseBindResponsePayload(payload)
	if err != nil {
		t.Fatalf("parseBindResponsePayload() error = %v", err)
	}
	if out.AgentToken != "t_123" || out.Endpoints.OpenClawPull != "/pull" || out.Endpoints.OpenClawPush != "/push" {
		t.Fatalf("unexpected bind response: %#v", out)
	}
}

func TestParseBindResponsePayloadPrefersRuntimeEndpointAliases(t *testing.T) {
	payload := json.RawMessage(`{
		"token":"t_123",
		"agent_uuid":"agent-1",
		"api_base":"https://na.hub.molten.bot",
		"endpoints":{
			"runtime_messages_pull":"/v1/runtime/messages/pull",
			"runtime_messages_publish":"/v1/runtime/messages/publish",
			"runtime_messages_offline":"/v1/runtime/messages/offline",
			"runtime_messages_ws":"/v1/runtime/messages/ws",
			"openclaw_messages_pull":"/v1/openclaw/messages/pull",
			"openclaw_messages_publish":"/v1/openclaw/messages/publish",
			"openclaw_offline":"/v1/openclaw/messages/offline"
		}
	}`)
	out, err := parseBindResponsePayload(payload)
	if err != nil {
		t.Fatalf("parseBindResponsePayload() error = %v", err)
	}
	if got, want := out.Endpoints.OpenClawPull, "/v1/runtime/messages/pull"; got != want {
		t.Fatalf("pull endpoint = %q, want %q", got, want)
	}
	if got, want := out.Endpoints.OpenClawPush, "/v1/runtime/messages/publish"; got != want {
		t.Fatalf("publish endpoint = %q, want %q", got, want)
	}
	if got, want := out.Endpoints.Offline, "/v1/runtime/messages/offline"; got != want {
		t.Fatalf("offline endpoint = %q, want %q", got, want)
	}
	if got, want := out.Endpoints.RuntimeWS, "/v1/runtime/messages/ws"; got != want {
		t.Fatalf("websocket endpoint = %q, want %q", got, want)
	}
}

func TestDeliveryEndpointFromPullUsesRuntimePath(t *testing.T) {
	got := deliveryEndpointFromPull("https://na.hub.molten.bot/v1/runtime/messages/pull", "ack")
	want := "https://na.hub.molten.bot/v1/runtime/messages/ack"
	if got != want {
		t.Fatalf("deliveryEndpointFromPull() = %q, want %q", got, want)
	}
}

func TestDeliveryEndpointFromPullKeepsLegacyPathCompatibility(t *testing.T) {
	got := deliveryEndpointFromPull("https://na.hub.molten.bot/v1/openclaw/messages/pull", "ack")
	want := "https://na.hub.molten.bot/v1/openclaw/messages/ack"
	if got != want {
		t.Fatalf("deliveryEndpointFromPull() = %q, want %q", got, want)
	}
}

func TestDefaultRuntimeEndpointsUseCanonicalRuntimePaths(t *testing.T) {
	client := NewClient("https://na.hub.molten.bot")

	if got, want := client.openClawDeliveryEndpoint("ack"), "/v1/runtime/messages/ack"; got != want {
		t.Fatalf("ack endpoint = %q, want %q", got, want)
	}
	if got, want := client.openClawDeliveryEndpoint("nack"), "/v1/runtime/messages/nack"; got != want {
		t.Fatalf("nack endpoint = %q, want %q", got, want)
	}
	if got, want := client.openClawWebsocketEndpoint(), "/v1/runtime/messages/ws"; got != want {
		t.Fatalf("websocket endpoint = %q, want %q", got, want)
	}
}

func TestWebsocketEndpointUsesExplicitRuntimeEndpoint(t *testing.T) {
	client := NewClient("https://na.hub.molten.bot")
	client.SetRuntimeEndpoints(RuntimeEndpoints{OpenClawWebsocketURL: "/v1/runtime/messages/ws?mode=explicit"})

	if got, want := client.openClawWebsocketEndpoint(), "/v1/runtime/messages/ws?mode=explicit"; got != want {
		t.Fatalf("websocket endpoint = %q, want %q", got, want)
	}
}

func TestDecodePullResponsePayloadAcceptsRuntimeEnvelope(t *testing.T) {
	payload := json.RawMessage(`{
		"delivery_id":"delivery-1",
		"message_id":"message-1",
		"envelope":{"type":"skill_request","kind":"skill_activation","request_id":"request-1"}
	}`)

	got, err := decodePullResponsePayload(payload, "runtime response")
	if err != nil {
		t.Fatalf("decodePullResponsePayload() error = %v", err)
	}
	if got.DeliveryID != "delivery-1" || got.MessageID != "message-1" {
		t.Fatalf("unexpected delivery metadata: %#v", got)
	}
	if got.OpenClawMessage.Type != "skill_request" || got.OpenClawMessage.Kind != "skill_activation" || got.OpenClawMessage.RequestID != "request-1" {
		t.Fatalf("runtime envelope not mapped: %#v", got.OpenClawMessage)
	}
}

func TestDecodeAPIErrorCapturesCanonicalDetail(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusGone,
		Body: io.NopCloser(strings.NewReader(`{
			"error":"endpoint_retired",
			"message":"retired",
			"error_detail":{"replacement_endpoint":"/v1/runtime/messages/pull"}
		}`)),
	}

	err := decodeAPIError(resp)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("decodeAPIError() = %T, want *APIError", err)
	}
	detail, ok := apiErr.Detail.(map[string]any)
	if !ok || detail["replacement_endpoint"] != "/v1/runtime/messages/pull" {
		t.Fatalf("error detail = %#v", apiErr.Detail)
	}
}

func TestNewRequestAbsoluteEndpointIgnoresBasePath(t *testing.T) {
	client := NewClient("https://na.hub.molten.bot/api")

	req, err := client.newRequest(context.Background(), http.MethodPatch, "/v1/agents/me/metadata", "t_123", nil)
	if err != nil {
		t.Fatalf("newRequest() error = %v", err)
	}
	if got, want := req.URL.String(), "https://na.hub.molten.bot/v1/agents/me/metadata"; got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
}

func TestNewRequestRelativeEndpointKeepsBasePath(t *testing.T) {
	client := NewClient("https://na.hub.molten.bot/api")

	req, err := client.newRequest(context.Background(), http.MethodGet, "health", "", nil)
	if err != nil {
		t.Fatalf("newRequest() error = %v", err)
	}
	if got, want := req.URL.String(), "https://na.hub.molten.bot/api/health"; got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
}
