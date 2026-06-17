//go:build ignore

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
)

type csrReq struct {
	CSR string `json:"csr"`
}
type csrResp struct {
	Certificate string `json:"certificate"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: enroll <deviceID> <rawToken>")
		return
	}
	deviceID := os.Args[1]
	rawToken := os.Args[2]

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   deviceID,
			Organization: []string{"Themisto Dev Org"},
		},
	}
	csrBytes, _ := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, key)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	req, _ := http.NewRequest("POST", "http://localhost:8443/api/v1/devices/"+deviceID+"/csr", bytes.NewReader(csrPEM))
	req.Header.Set("X-Enrollment-Token", rawToken)
	req.Header.Set("Content-Type", "application/x-pem-file")

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		panic(fmt.Sprintf("Failed: %d %s", resp.StatusCode, string(body)))
	}

	var parsed csrResp
	json.Unmarshal(body, &parsed)

	os.WriteFile("agent.crt", []byte(parsed.Certificate), 0644)
	os.WriteFile("agent.key", keyPEM, 0600)
	fmt.Println("Success! Wrote agent.crt and agent.key")
}
