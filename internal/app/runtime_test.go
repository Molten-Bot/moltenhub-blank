package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newLoopbackServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on 127.0.0.1:0: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestFetchHubRuntimeCatalog(t *testing.T) {
	server := newLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
				{"display":"North America","key":"na","domain":"na.hub.molten.bot"},
				{"display":"Europe","key":"eu","domain":"eu.hub.molten.bot"},
				{"display":"Bad","key":"bad","domain":"example.com"}
			]`))
	}))

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
