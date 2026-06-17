package mtls

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CertVerifier struct {
	backendURL    string
	failMode      string
	internalToken string
	controlURL    string
	controlToken  string
	controlFail   string
	client        *http.Client
	logger        *slog.Logger

	mu         sync.RWMutex
	cache      map[string]*cacheEntry
	maxSize    int
	cacheTTL   time.Duration
	controlTTL time.Duration
}

type cacheEntry struct {
	status       string
	deviceID     string
	orgID        string
	deviceStatus string
	orgStatus    string
	expiresAt    time.Time
}

type certStatusResponse struct {
	Serial   string `json:"serial"`
	Status   string `json:"status"`
	DeviceID string `json:"device_id"`
	OrgID    string `json:"org_id"`
}

type orgStatusResponse struct {
	Status string `json:"status"`
}

type deviceStatusResponse struct {
	Status             string `json:"status"`
	OrganizationStatus string `json:"organization_status"`
}

func NewCertVerifier(backendURL, failMode, internalToken, controlURL, controlToken, controlFail string, controlTTL, cacheTTL time.Duration, maxSize int, logger *slog.Logger) *CertVerifier {
	if strings.TrimSpace(controlFail) == "" {
		controlFail = failMode
	}
	return &CertVerifier{
		backendURL:    backendURL,
		failMode:      failMode,
		internalToken: internalToken,
		controlURL:    strings.TrimRight(strings.TrimSpace(controlURL), "/"),
		controlToken:  controlToken,
		controlFail:   controlFail,
		client:        &http.Client{Timeout: 5 * time.Second},
		logger:        logger,
		cache:         make(map[string]*cacheEntry),
		maxSize:       maxSize,
		cacheTTL:      cacheTTL,
		controlTTL:    controlTTL,
	}
}

func isValidHexSerial(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (v *CertVerifier) IsRevoked(serial string) (bool, error) {
	if !isValidHexSerial(serial) {
		return true, fmt.Errorf("invalid serial format")
	}

	v.mu.RLock()
	if entry, ok := v.cache[serial]; ok && time.Now().Before(entry.expiresAt) {
		v.mu.RUnlock()
		return entry.status == "revoked", nil
	}
	v.mu.RUnlock()

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/internal/cert-status/%s", v.backendURL, serial), nil)
	if err != nil {
		return v.failMode == "closed", fmt.Errorf("build request: %w", err)
	}
	if v.internalToken != "" {
		req.Header.Set("X-Internal-Token", v.internalToken)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		v.logger.Error("cert status check failed", "serial", serial, "error", err)
		return v.failMode == "closed", fmt.Errorf("backend unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		v.cacheStatus(serial, "unknown", "", "", "unknown", "unknown")
		return true, nil
	}
	if resp.StatusCode != http.StatusOK {
		v.logger.Error("cert status unexpected response", "serial", serial, "status", resp.StatusCode)
		return v.failMode == "closed", nil
	}

	var status certStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		v.logger.Error("cert status decode error", "serial", serial, "error", err)
		return v.failMode == "closed", nil
	}

	revoked := status.Status == "revoked"
	deviceStatus := "active"
	orgStatus := "active"
	if !revoked && v.controlURL != "" && status.DeviceID != "" && status.OrgID != "" {
		allowed, resolvedDeviceStatus, resolvedOrgStatus, controlErr := v.checkControlPlane(status.DeviceID, status.OrgID)
		if controlErr != nil {
			v.logger.Error("control plane enforcement failed", "serial", serial, "device_id", status.DeviceID, "org_id", status.OrgID, "error", controlErr)
			if strings.ToLower(strings.TrimSpace(v.controlFail)) == "closed" {
				revoked = true
				deviceStatus = "unknown"
				orgStatus = "unknown"
			}
		} else {
			deviceStatus = resolvedDeviceStatus
			orgStatus = resolvedOrgStatus
			if !allowed {
				revoked = true
			}
		}
	}

	cacheStatus := status.Status
	if revoked && cacheStatus != "revoked" {
		cacheStatus = "revoked"
	}
	v.cacheStatus(serial, cacheStatus, status.DeviceID, status.OrgID, deviceStatus, orgStatus)
	return revoked, nil
}

// LookupIdentity returns cached (deviceID, orgID) for a certificate serial.
// Data is returned only when the cache entry exists, is unexpired, and both
// identifiers are present.
func (v *CertVerifier) LookupIdentity(serial string) (deviceID, orgID string, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	entry, found := v.cache[serial]
	if !found || time.Now().After(entry.expiresAt) {
		return "", "", false
	}
	if entry.deviceID == "" || entry.orgID == "" {
		return "", "", false
	}
	return entry.deviceID, entry.orgID, true
}

func (v *CertVerifier) cacheStatus(serial, status, deviceID, orgID, deviceStatus, orgStatus string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.cache) >= v.maxSize {
		oldest := ""
		oldestTime := time.Now().Add(time.Hour)
		for k, e := range v.cache {
			if e.expiresAt.Before(oldestTime) {
				oldest = k
				oldestTime = e.expiresAt
			}
		}
		if oldest != "" {
			delete(v.cache, oldest)
		}
	}

	ttl := v.cacheTTL
	if v.controlURL != "" && v.controlTTL > 0 && (ttl <= 0 || v.controlTTL < ttl) {
		ttl = v.controlTTL
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	v.cache[serial] = &cacheEntry{
		status:       status,
		deviceID:     deviceID,
		orgID:        orgID,
		deviceStatus: deviceStatus,
		orgStatus:    orgStatus,
		expiresAt:    time.Now().Add(ttl),
	}
}

func (v *CertVerifier) checkControlPlane(deviceID, orgID string) (bool, string, string, error) {
	orgReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/enforcement/org-status/%s", v.controlURL, orgID), nil)
	if err != nil {
		return false, "", "", fmt.Errorf("build org-status request: %w", err)
	}
	deviceReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/enforcement/device-status/%s", v.controlURL, deviceID), nil)
	if err != nil {
		return false, "", "", fmt.Errorf("build device-status request: %w", err)
	}
	if v.controlToken != "" {
		orgReq.Header.Set("Authorization", "Bearer "+v.controlToken)
		deviceReq.Header.Set("Authorization", "Bearer "+v.controlToken)
	}

	orgResp, err := v.client.Do(orgReq)
	if err != nil {
		return false, "", "", fmt.Errorf("fetch org-status: %w", err)
	}
	defer orgResp.Body.Close()
	if orgResp.StatusCode == http.StatusNotFound {
		return false, "unknown", "unknown", nil
	}
	if orgResp.StatusCode != http.StatusOK {
		return false, "", "", fmt.Errorf("org-status returned %d", orgResp.StatusCode)
	}
	var orgPayload orgStatusResponse
	if err := json.NewDecoder(orgResp.Body).Decode(&orgPayload); err != nil {
		return false, "", "", fmt.Errorf("decode org-status: %w", err)
	}

	deviceResp, err := v.client.Do(deviceReq)
	if err != nil {
		return false, "", "", fmt.Errorf("fetch device-status: %w", err)
	}
	defer deviceResp.Body.Close()
	if deviceResp.StatusCode == http.StatusNotFound {
		return false, "unknown", strings.ToLower(strings.TrimSpace(orgPayload.Status)), nil
	}
	if deviceResp.StatusCode != http.StatusOK {
		return false, "", "", fmt.Errorf("device-status returned %d", deviceResp.StatusCode)
	}
	var devicePayload deviceStatusResponse
	if err := json.NewDecoder(deviceResp.Body).Decode(&devicePayload); err != nil {
		return false, "", "", fmt.Errorf("decode device-status: %w", err)
	}

	orgStatus := strings.ToLower(strings.TrimSpace(orgPayload.Status))
	deviceStatus := strings.ToLower(strings.TrimSpace(devicePayload.Status))
	if orgStatus == "" {
		orgStatus = strings.ToLower(strings.TrimSpace(devicePayload.OrganizationStatus))
	}
	if orgStatus == "" {
		orgStatus = "unknown"
	}
	if deviceStatus == "" {
		deviceStatus = "unknown"
	}
	return orgStatus == "active" && deviceStatus == "active", deviceStatus, orgStatus, nil
}
