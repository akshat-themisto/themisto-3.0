//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const devOrgID = "a0000000-0000-0000-0000-000000000001"

type registerResponse struct {
	DeviceID        string `json:"device_id"`
	EnrollmentToken string `json:"enrollment_token"`
}

type csrResponse struct {
	Certificate string `json:"certificate"`
	CAChain     string `json:"ca_chain"`
}

func main() {
	backendURL := flag.String("backend-url", "http://localhost:8643", "backend URL")
	apiKey := flag.String("api-key", "themisto_dev_admin_key_123", "admin API key")
	deviceName := flag.String("device-name", "themisto-dev-windows", "device name")
	osName := flag.String("os", "windows", "device OS")
	outDir := flag.String("out-dir", filepath.Join("certs", "devices", "themisto-dev-windows"), "output directory")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reg, err := registerDevice(ctx, *backendURL, *apiKey, *deviceName, *osName)
	if err != nil {
		fatal("register device", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fatal("generate key", err)
	}
	csrPEM, err := createCSR(key, reg.DeviceID)
	if err != nil {
		fatal("create csr", err)
	}
	signed, err := submitCSR(ctx, *backendURL, reg.DeviceID, reg.EnrollmentToken, csrPEM)
	if err != nil {
		fatal("submit csr", err)
	}
	if err := os.MkdirAll(*outDir, 0700); err != nil {
		fatal("create output dir", err)
	}
	if err := writeKey(filepath.Join(*outDir, "device.key"), key); err != nil {
		fatal("write key", err)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "device.crt"), []byte(signed.Certificate), 0444); err != nil {
		fatal("write cert", err)
	}
	if err := os.WriteFile(filepath.Join(*outDir, "ca-chain.pem"), []byte(signed.CAChain), 0444); err != nil {
		fatal("write ca chain", err)
	}
	fmt.Printf("Device enrolled: %s\n", reg.DeviceID)
	fmt.Printf("Credentials written to: %s\n", *outDir)
}

func registerDevice(ctx context.Context, backendURL, apiKey, deviceName, osName string) (*registerResponse, error) {
	body := map[string]string{
		"org_id":        devOrgID,
		"device_name":   deviceName,
		"os":            osName,
		"agent_version": "0.1.0",
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/api/v1/devices", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	var out registerResponse
	if err := doJSON(req, &out); err != nil {
		return nil, err
	}
	if out.DeviceID == "" || out.EnrollmentToken == "" {
		return nil, fmt.Errorf("registration response missing device_id or enrollment_token")
	}
	return &out, nil
}

func createCSR(key *ecdsa.PrivateKey, deviceID string) ([]byte, error) {
	tpl := &x509.CertificateRequest{
		Subject: pkixName("Themisto Dev Org", deviceID),
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tpl, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func submitCSR(ctx context.Context, backendURL, deviceID, token string, csrPEM []byte) (*csrResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/api/v1/devices/"+deviceID+"/csr", bytes.NewReader(csrPEM))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Enrollment-Token", token)
	req.Header.Set("Content-Type", "application/pkcs10")
	var out csrResponse
	if err := doJSON(req, &out); err != nil {
		return nil, err
	}
	if out.Certificate == "" || out.CAChain == "" {
		return nil, fmt.Errorf("csr response missing certificate or ca_chain")
	}
	return &out, nil
}

func doJSON(req *http.Request, out interface{}) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0400)
}

func pkixName(org, cn string) pkix.Name {
	return pkix.Name{
		Organization: []string{org},
		CommonName:   cn,
		SerialNumber: new(big.Int).SetBytes([]byte(cn)).Text(16),
	}
}

func fatal(label string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
	os.Exit(1)
}
