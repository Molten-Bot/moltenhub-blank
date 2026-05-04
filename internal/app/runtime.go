package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	hubBaseDomain = "hub.molten.bot"
	hubCatalogURL = "https://molten.bot/hubs.json"
)

type HubRuntime struct {
	ID          string
	Label       string
	Description string
	HubURL      string
}

var fallbackHubRuntimes = []HubRuntime{
	{ID: "na", Label: "NA", Description: "North America", HubURL: "https://na.hub.molten.bot"},
	{ID: "eu", Label: "EU", Description: "Europe", HubURL: "https://eu.hub.molten.bot"},
}

var (
	hubRuntimeCatalogClient = &http.Client{Timeout: 2 * time.Second}
	hubRuntimeCatalogMu     sync.RWMutex
	hubRuntimeCatalogLoaded bool
	hubRuntimeCatalog       = cloneHubRuntimes(fallbackHubRuntimes)
)

func SupportedHubRuntimes() []HubRuntime {
	return cloneHubRuntimes(currentHubRuntimes())
}

func DefaultHubRuntime() HubRuntime {
	runtimes := currentHubRuntimes()
	if len(runtimes) == 0 {
		return HubRuntime{}
	}
	return runtimes[0]
}

func ResolveHubRuntime(region, hubURL string) (HubRuntime, error) {
	if runtime, ok := hubRuntimeByID(region); ok {
		return runtime, nil
	}
	if runtime, ok := hubRuntimeByURL(hubURL); ok {
		return runtime, nil
	}
	if strings.TrimSpace(region) == "" && strings.TrimSpace(hubURL) == "" {
		return DefaultHubRuntime(), nil
	}
	return HubRuntime{}, fmt.Errorf("unsupported hub runtime selection %q (%q)", strings.TrimSpace(region), strings.TrimSpace(hubURL))
}

func NormalizeHubEndpointURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Port() != "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if _, ok := runtimeFromHost(host); !ok {
		return ""
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.User = nil
	return strings.TrimRight(parsed.String(), "/")
}

func currentHubRuntimes() []HubRuntime {
	hubRuntimeCatalogMu.RLock()
	if hubRuntimeCatalogLoaded {
		runtimes := cloneHubRuntimes(hubRuntimeCatalog)
		hubRuntimeCatalogMu.RUnlock()
		return runtimes
	}
	hubRuntimeCatalogMu.RUnlock()

	hubRuntimeCatalogMu.Lock()
	defer hubRuntimeCatalogMu.Unlock()
	if !hubRuntimeCatalogLoaded {
		if runtimes, err := fetchHubRuntimeCatalog(hubCatalogURL, hubRuntimeCatalogClient); err == nil && len(runtimes) > 0 {
			hubRuntimeCatalog = runtimes
		} else {
			hubRuntimeCatalog = cloneHubRuntimes(fallbackHubRuntimes)
		}
		hubRuntimeCatalogLoaded = true
	}
	return cloneHubRuntimes(hubRuntimeCatalog)
}

func fetchHubRuntimeCatalog(rawURL string, client *http.Client) ([]HubRuntime, error) {
	if client == nil {
		client = hubRuntimeCatalogClient
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub catalog returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload []struct {
		Display string `json:"display"`
		Key     string `json:"key"`
		Domain  string `json:"domain"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	runtimes := make([]HubRuntime, 0, len(payload))
	seen := make(map[string]struct{}, len(payload))
	for _, item := range payload {
		id := strings.ToLower(strings.TrimSpace(item.Key))
		if id == "" {
			continue
		}
		hubURL := catalogHubURL(item.Domain)
		if hubURL == "" || strings.TrimSpace(item.Display) == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		runtimes = append(runtimes, HubRuntime{ID: id, Label: strings.ToUpper(id), Description: strings.TrimSpace(item.Display), HubURL: hubURL})
	}
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("hub catalog %q did not contain any supported runtimes", rawURL)
	}
	return runtimes, nil
}

func hubRuntimeByID(region string) (HubRuntime, bool) {
	region = strings.ToLower(strings.TrimSpace(region))
	for _, runtime := range currentHubRuntimes() {
		if runtime.ID == region {
			return runtime, true
		}
	}
	return HubRuntime{}, false
}

func hubRuntimeByURL(hubURL string) (HubRuntime, bool) {
	hubURL = normalizeHubRuntimeURL(hubURL)
	for _, runtime := range currentHubRuntimes() {
		if normalizeHubRuntimeURL(runtime.HubURL) == hubURL {
			return runtime, true
		}
	}
	return HubRuntime{}, false
}

func normalizeHubRuntimeURL(raw string) string {
	normalized := NormalizeHubEndpointURL(raw)
	if normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Host == "" {
		return ""
	}
	runtime, ok := runtimeFromHost(parsed.Hostname())
	if !ok {
		return ""
	}
	return runtime.HubURL
}

func runtimeFromHost(host string) (HubRuntime, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, runtime := range currentHubRuntimes() {
		root := strings.ToLower(runtime.ID + "." + hubBaseDomain)
		if host == root || strings.HasSuffix(host, "."+root) {
			return runtime, true
		}
	}
	return HubRuntime{}, false
}

func catalogHubURL(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || !strings.HasSuffix(domain, "."+hubBaseDomain) {
		return ""
	}
	parsed, err := url.Parse("https://" + domain)
	if err != nil || parsed.Hostname() != domain || parsed.User != nil || parsed.Port() != "" {
		return ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

func cloneHubRuntimes(runtimes []HubRuntime) []HubRuntime {
	cloned := make([]HubRuntime, len(runtimes))
	copy(cloned, runtimes)
	return cloned
}

func defaultAPIBaseForHub(hubURL string) string {
	return NormalizeHubEndpointURL(hubURL)
}
