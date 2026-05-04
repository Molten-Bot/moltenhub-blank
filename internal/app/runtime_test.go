package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchHubRuntimeCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"display":"North America","key":"na","domain":"na.hub.molten.bot"},
			{"display":"Europe","key":"eu","domain":"eu.hub.molten.bot"},
			{"display":"Bad","key":"bad","domain":"example.com"}
		]`))
	}))
	defer server.Close()

	runtimes, err := fetchHubRuntimeCatalog(server.URL, server.Client())
	if err != nil {
		t.Fatalf("fetchHubRuntimeCatalog() error = %v", err)
	}
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(runtimes))
	}
	if runtimes[0].ID != "na" || runtimes[0].HubURL != "https://na.hub.molten.bot" {
		t.Fatalf("unexpected first runtime: %#v", runtimes[0])
	}
}

func TestNormalizeHubEndpointURLRejectsUnsupportedHosts(t *testing.T) {
	if got := NormalizeHubEndpointURL("https://example.com"); got != "" {
		t.Fatalf("expected unsupported host to be rejected, got %q", got)
	}
	if got := NormalizeHubEndpointURL("https://na.hub.molten.bot/api"); got != "https://na.hub.molten.bot/api" {
		t.Fatalf("expected allowed hub URL, got %q", got)
	}
}
