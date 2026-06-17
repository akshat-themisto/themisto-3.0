package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrDenied = errors.New("control plane denied access")

type Client struct {
	baseURL  string
	token    string
	failMode string
	client   *http.Client
	cacheTTL time.Duration

	mu          sync.RWMutex
	orgCache    map[string]orgCacheEntry
	deviceCache map[string]deviceCacheEntry
}

type orgCacheEntry struct {
	status    string
	reason    string
	expiresAt time.Time
}

type deviceCacheEntry struct {
	status             string
	reason             string
	organizationStatus string
	expiresAt          time.Time
}

type orgStatusResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type deviceStatusResponse struct {
	Status             string `json:"status"`
	StatusReason       string `json:"status_reason"`
	OrganizationStatus string `json:"organization_status"`
}

func NewClient(baseURL, token, failMode string, cacheTTL time.Duration) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if cacheTTL <= 0 {
		cacheTTL = 30 * time.Second
	}
	failMode = strings.ToLower(strings.TrimSpace(failMode))
	if failMode != "open" {
		failMode = "closed"
	}
	return &Client{
		baseURL:     baseURL,
		token:       token,
		failMode:    failMode,
		client:      &http.Client{Timeout: 5 * time.Second},
		cacheTTL:    cacheTTL,
		orgCache:    make(map[string]orgCacheEntry),
		deviceCache: make(map[string]deviceCacheEntry),
	}
}

func (c *Client) EnforceOrganizationActive(ctx context.Context, orgID string) error {
	if c == nil || strings.TrimSpace(orgID) == "" {
		return nil
	}
	entry, err := c.lookupOrg(ctx, orgID)
	if err != nil {
		if c.failMode == "open" {
			return nil
		}
		return err
	}
	if entry.status != "active" {
		return fmt.Errorf("%w: organization status=%s reason=%s", ErrDenied, entry.status, entry.reason)
	}
	return nil
}

func (c *Client) EnforceDeviceActive(ctx context.Context, deviceID string) error {
	if c == nil || strings.TrimSpace(deviceID) == "" {
		return nil
	}
	entry, err := c.lookupDevice(ctx, deviceID)
	if err != nil {
		if c.failMode == "open" {
			return nil
		}
		return err
	}
	if entry.organizationStatus != "" && entry.organizationStatus != "active" {
		return fmt.Errorf("%w: organization status=%s reason=%s", ErrDenied, entry.organizationStatus, entry.reason)
	}
	if entry.status != "active" {
		return fmt.Errorf("%w: device status=%s reason=%s", ErrDenied, entry.status, entry.reason)
	}
	return nil
}

func (c *Client) lookupOrg(ctx context.Context, orgID string) (orgCacheEntry, error) {
	c.mu.RLock()
	entry, ok := c.orgCache[orgID]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/enforcement/org-status/"+orgID, nil)
	if err != nil {
		return orgCacheEntry{}, fmt.Errorf("build org-status request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return orgCacheEntry{}, fmt.Errorf("fetch org-status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return orgCacheEntry{}, fmt.Errorf("%w: organization not found", ErrDenied)
	}
	if resp.StatusCode != http.StatusOK {
		return orgCacheEntry{}, fmt.Errorf("org-status returned %d", resp.StatusCode)
	}
	var payload orgStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return orgCacheEntry{}, fmt.Errorf("decode org-status: %w", err)
	}
	entry = orgCacheEntry{
		status:    strings.ToLower(strings.TrimSpace(payload.Status)),
		reason:    strings.TrimSpace(payload.Reason),
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.mu.Lock()
	c.orgCache[orgID] = entry
	c.mu.Unlock()
	return entry, nil
}

func (c *Client) lookupDevice(ctx context.Context, deviceID string) (deviceCacheEntry, error) {
	c.mu.RLock()
	entry, ok := c.deviceCache[deviceID]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/enforcement/device-status/"+deviceID, nil)
	if err != nil {
		return deviceCacheEntry{}, fmt.Errorf("build device-status request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return deviceCacheEntry{}, fmt.Errorf("fetch device-status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return deviceCacheEntry{}, fmt.Errorf("%w: device not found", ErrDenied)
	}
	if resp.StatusCode != http.StatusOK {
		return deviceCacheEntry{}, fmt.Errorf("device-status returned %d", resp.StatusCode)
	}
	var payload deviceStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return deviceCacheEntry{}, fmt.Errorf("decode device-status: %w", err)
	}
	entry = deviceCacheEntry{
		status:             strings.ToLower(strings.TrimSpace(payload.Status)),
		reason:             strings.TrimSpace(payload.StatusReason),
		organizationStatus: strings.ToLower(strings.TrimSpace(payload.OrganizationStatus)),
		expiresAt:          time.Now().Add(c.cacheTTL),
	}
	c.mu.Lock()
	c.deviceCache[deviceID] = entry
	c.mu.Unlock()
	return entry, nil
}
