// Package beszel is a client for a self-hosted Beszel
// (https://github.com/henrygd/beszel) instance's PocketBase-backed API. It
// uses a pre-generated auth token and looks up whether a named
// system/container is healthy.
//
// --- IMPORTANT: verify your schema first ---
// Beszel's API is built on PocketBase and its exact field names can change
// between versions. Before relying on this, check what your own instance
// actually returns:
//
//	curl -s "http://<beszel-host>:8090/api/collections/containers/records" \
//	  -H "Authorization: Bearer $BESZEL_TOKEN" | jq .
//
// Look at the "status" (or equivalent) field on a container record and
// adjust healthyValues below if needed.
package beszel

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 15 * time.Second

// Adjust this if your Beszel version reports container status differently.
var healthyValues = map[string]bool{
	"up":      true,
	"healthy": true,
	"running": true,
}

type cacheEntry struct {
	expires time.Time
	code    int
}

type listResponse struct {
	Items []map[string]any `json:"items"`
}

// Token is a pre-generated PocketBase auth token for a Beszel instance.
type Token string

// Client holds a Beszel API session and caches to avoid hammering it.
type Client struct {
	baseURL string
	token   Token
	client  *http.Client

	mu            sync.Mutex
	systemIDCache map[string]string

	resultMu    sync.Mutex
	resultCache map[string]cacheEntry
}

// New creates a Client. baseURL is the root URL of the Beszel instance,
// token is a pre-generated PocketBase auth token (see README for how to
// get one).
func New(baseURL string, token Token) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		token:         token,
		client:        &http.Client{Timeout: 5 * time.Second},
		systemIDCache: make(map[string]string),
		resultCache:   make(map[string]cacheEntry),
	}
}

func (c *Client) pbGet(path string) (*listResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(c.token))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("beszel returned %s", resp.Status)
	}

	var out listResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getSystemID(name string) (string, error) {
	c.mu.Lock()
	if id, ok := c.systemIDCache[name]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	filter := fmt.Sprintf("name='%s'", name)
	path := "/api/collections/systems/records?filter=" + url.QueryEscape(filter)

	data, err := c.pbGet(path)
	if err != nil {
		return "", err
	}
	if len(data.Items) == 0 {
		return "", nil // not found - caller treats as 404
	}

	id, _ := data.Items[0]["id"].(string)
	c.mu.Lock()
	c.systemIDCache[name] = id
	c.mu.Unlock()
	return id, nil
}

// CheckContainer returns the HTTP status code to report for this container,
// using a short-lived cache so a burst of Dashy refreshes doesn't hammer
// Beszel.
func (c *Client) CheckContainer(system, container string) int {
	cacheKey := system + "/" + container

	c.resultMu.Lock()
	if entry, ok := c.resultCache[cacheKey]; ok && time.Now().Before(entry.expires) {
		c.resultMu.Unlock()
		return entry.code
	}
	c.resultMu.Unlock()

	code := c.checkContainerUncached(system, container)

	c.resultMu.Lock()
	c.resultCache[cacheKey] = cacheEntry{expires: time.Now().Add(cacheTTL), code: code}
	c.resultMu.Unlock()

	return code
}

func (c *Client) checkContainerUncached(system, container string) int {
	systemID, err := c.getSystemID(system)
	if err != nil {
		log.Printf("lookup system %q: %v", system, err)
		return http.StatusBadGateway
	}
	if systemID == "" {
		return http.StatusNotFound
	}

	filter := fmt.Sprintf("system='%s' && name='%s'", systemID, container)
	path := "/api/collections/containers/records?filter=" + url.QueryEscape(filter)

	data, err := c.pbGet(path)
	if err != nil {
		log.Printf("lookup container %q on %q: %v", container, system, err)
		return http.StatusBadGateway
	}
	if len(data.Items) == 0 {
		return http.StatusNotFound
	}

	status, _ := data.Items[0]["status"].(string)
	if healthyValues[strings.ToLower(status)] {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}
