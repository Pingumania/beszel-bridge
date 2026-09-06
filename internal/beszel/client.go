// Package beszel is a client for a self-hosted Beszel
// (https://github.com/henrygd/beszel) instance's PocketBase-backed API. It
// authenticates once and looks up whether a named system/container is
// healthy.
//
// --- IMPORTANT: verify your schema first ---
// Beszel's API is built on PocketBase and its exact field names can change
// between versions. Before relying on this, check what your own instance
// actually returns:
//
//	TOKEN=$(curl -s -X POST http://<beszel-host>:8090/api/collections/users/auth-with-password \
//	  -H "Content-Type: application/json" \
//	  -d '{"identity":"you@example.com","password":"yourpassword"}' | jq -r .token)
//
//	curl -s "http://<beszel-host>:8090/api/collections/containers/records" \
//	  -H "Authorization: Bearer $TOKEN" | jq .
//
// Look at the "status" (or equivalent) field on a container record and
// adjust healthyValues below if needed.
package beszel

import (
	"bytes"
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

type authResponse struct {
	Token string `json:"token"`
}

type listResponse struct {
	Items []map[string]any `json:"items"`
}

// Client holds a Beszel API session and caches to avoid hammering it.
type Client struct {
	baseURL  string
	email    string
	password string
	client   *http.Client

	mu            sync.Mutex
	token         string
	tokenExpires  time.Time
	systemIDCache map[string]string

	resultMu    sync.Mutex
	resultCache map[string]cacheEntry
}

// New creates a Client. baseURL is the root URL of the Beszel instance.
func New(baseURL, email, password string) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		email:         email,
		password:      password,
		client:        &http.Client{Timeout: 5 * time.Second},
		systemIDCache: make(map[string]string),
		resultCache:   make(map[string]cacheEntry),
	}
}

func (c *Client) getToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpires) {
		return c.token, nil
	}

	body, err := json.Marshal(map[string]string{
		"identity": c.email,
		"password": c.password,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/api/collections/users/auth-with-password",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("beszel auth failed: %s", resp.Status)
	}

	var auth authResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return "", err
	}

	c.token = auth.Token
	// PocketBase tokens are long-lived; refresh well before they'd expire.
	c.tokenExpires = time.Now().Add(50 * time.Minute)
	return c.token, nil
}

func (c *Client) pbGet(path, token string) (*listResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

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

	token, err := c.getToken()
	if err != nil {
		return "", err
	}

	filter := fmt.Sprintf("name='%s'", name)
	path := "/api/collections/systems/records?filter=" + url.QueryEscape(filter)

	data, err := c.pbGet(path, token)
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

	token, err := c.getToken()
	if err != nil {
		log.Printf("get token: %v", err)
		return http.StatusBadGateway
	}

	filter := fmt.Sprintf("system='%s' && name='%s'", systemID, container)
	path := "/api/collections/containers/records?filter=" + url.QueryEscape(filter)

	data, err := c.pbGet(path, token)
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
