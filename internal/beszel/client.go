// Package beszel is a client for a self-hosted Beszel
// (https://github.com/henrygd/beszel) instance's PocketBase-backed API. It
// authenticates with a Beszel user's email/password and looks up whether a
// named system/container is healthy.
package beszel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	cacheTTL        = 15 * time.Second
	systemIDTTL     = 5 * time.Minute
	maxCacheEntries = 1000
)

// validNamePattern restricts system/container names to a safe charset so
// they can't break out of the quoted string literals in PocketBase filter
// expressions built by fmt.Sprintf below.
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validName(s string) bool {
	return validNamePattern.MatchString(s)
}

// pruneExpired removes expired entries from m, then clears it entirely if
// it's still at cap, bounding memory use under sustained cache-key churn.
func pruneExpired[K comparable, V any](m map[K]V, expiresOf func(V) time.Time) {
	now := time.Now()
	for k, v := range m {
		if now.After(expiresOf(v)) {
			delete(m, k)
		}
	}
	if len(m) >= maxCacheEntries {
		clear(m)
	}
}

func isHealthy(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(status, "up"):
		return true
	case status == "healthy", status == "running":
		return true
	default:
		return false
	}
}

type cacheEntry struct {
	expires time.Time
	code    int
}

type systemIDEntry struct {
	expires time.Time
	id      string
}

type listResponse struct {
	Items []map[string]any `json:"items"`
}

// Target identifies a container to check, by its Beszel system and
// container name.
type Target struct {
	System    string
	Container string
}

func (t Target) String() string {
	return t.System + "/" + t.Container
}

type Credentials struct {
	Email    string
	Password string
}

// Client holds a Beszel API session and caches to avoid hammering it.
type Client struct {
	baseURL string
	creds   Credentials
	client  *http.Client

	authMu sync.Mutex
	token  string

	mu            sync.Mutex
	systemIDCache map[string]systemIDEntry

	resultMu    sync.Mutex
	resultCache map[string]cacheEntry
}

func New(baseURL string, creds Credentials) *Client {
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		creds:         creds,
		client:        &http.Client{Timeout: 5 * time.Second},
		systemIDCache: make(map[string]systemIDEntry),
		resultCache:   make(map[string]cacheEntry),
	}
}

type authResponse struct {
	Token string `json:"token"`
}

// authenticateLocked requires authMu held.
func (c *Client) authenticateLocked() error {
	payload, err := json.Marshal(map[string]string{
		"identity": c.creds.Email,
		"password": c.creds.Password,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("beszel auth returned %s", resp.Status)
	}

	var out authResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}

	c.token = out.Token
	return nil
}

func (c *Client) Authenticate() error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.authenticateLocked()
}

// ensureToken re-authenticates unless the cached token has already moved
// past stale.
func (c *Client) ensureToken(stale string) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.token != "" && c.token != stale {
		return c.token, nil
	}
	if err := c.authenticateLocked(); err != nil {
		return "", err
	}
	return c.token, nil
}

// rawGet performs an authenticated GET and returns the raw body and status
// code, without treating a non-200 status as an error.
func (c *Client) rawGet(path, token string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

func (c *Client) pbGet(path string) (*listResponse, error) {
	token, err := c.ensureToken("")
	if err != nil {
		return nil, err
	}

	body, status, err := c.rawGet(path, token)
	if err != nil {
		return nil, err
	}

	if status == http.StatusUnauthorized {
		token, err = c.ensureToken(token)
		if err != nil {
			return nil, fmt.Errorf("re-authenticate: %w", err)
		}
		body, status, err = c.rawGet(path, token)
		if err != nil {
			return nil, err
		}
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("beszel returned status %d", status)
	}

	var out listResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// queryFirst returns the first record in collection matching filter, or nil
// if none match.
func (c *Client) queryFirst(collection, filter string) (map[string]any, error) {
	path := fmt.Sprintf("/api/collections/%s/records?filter=%s", collection, url.QueryEscape(filter))

	data, err := c.pbGet(path)
	if err != nil {
		return nil, err
	}
	if len(data.Items) == 0 {
		return nil, nil
	}
	return data.Items[0], nil
}

func (c *Client) getSystemID(name string) (string, error) {
	c.mu.Lock()
	if entry, ok := c.systemIDCache[name]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.id, nil
	}
	c.mu.Unlock()

	record, err := c.queryFirst("systems", fmt.Sprintf("name='%s'", name))
	if err != nil {
		return "", err
	}
	if record == nil {
		return "", nil // not found - caller treats as 404
	}

	id, _ := record["id"].(string)
	c.mu.Lock()
	pruneExpired(c.systemIDCache, func(e systemIDEntry) time.Time { return e.expires })
	c.systemIDCache[name] = systemIDEntry{expires: time.Now().Add(systemIDTTL), id: id}
	c.mu.Unlock()
	return id, nil
}

// CheckContainer returns the HTTP status code to report for this container,
// using a short-lived cache so a burst of Dashy refreshes doesn't hammer
// Beszel.
func (c *Client) CheckContainer(target Target) int {
	if !validName(target.System) || !validName(target.Container) {
		return http.StatusBadRequest
	}

	cacheKey := target.String()

	c.resultMu.Lock()
	if entry, ok := c.resultCache[cacheKey]; ok && time.Now().Before(entry.expires) {
		c.resultMu.Unlock()
		return entry.code
	}
	c.resultMu.Unlock()

	code := c.checkContainerUncached(target)

	c.resultMu.Lock()
	pruneExpired(c.resultCache, func(e cacheEntry) time.Time { return e.expires })
	c.resultCache[cacheKey] = cacheEntry{expires: time.Now().Add(cacheTTL), code: code}
	c.resultMu.Unlock()

	return code
}

func (c *Client) checkContainerUncached(target Target) int {
	systemID, err := c.getSystemID(target.System)
	if err != nil {
		log.Printf("lookup system %q: %v", target.System, err)
		return http.StatusBadGateway
	}
	if systemID == "" {
		return http.StatusNotFound
	}

	filter := fmt.Sprintf("system='%s' && name='%s'", systemID, target.Container)
	record, err := c.queryFirst("containers", filter)
	if err != nil {
		log.Printf("lookup container %q on %q: %v", target.Container, target.System, err)
		return http.StatusBadGateway
	}
	if record == nil {
		return http.StatusNotFound
	}

	status, _ := record["status"].(string)
	if isHealthy(status) {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}
