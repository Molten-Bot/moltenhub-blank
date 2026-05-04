package hub

import (
	"context"
	"encoding/json"
	"net/http"
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

func TestDeliveryEndpointFromPull(t *testing.T) {
	got := deliveryEndpointFromPull("https://na.hub.molten.bot/v1/openclaw/messages/pull", "ack")
	want := "https://na.hub.molten.bot/v1/openclaw/messages/ack"
	if got != want {
		t.Fatalf("deliveryEndpointFromPull() = %q, want %q", got, want)
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
