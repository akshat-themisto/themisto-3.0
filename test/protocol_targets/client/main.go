package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	var (
		httpsBase = flag.String("https_base", "https://127.0.0.1:18443", "HTTPS base URL")
		grpcAddr  = flag.String("grpc_addr", "127.0.0.1:18444", "gRPC target host:port")
	)
	flag.Parse()

	if err := runHTTPS(*httpsBase); err != nil {
		log.Fatalf("https test failed: %v", err)
	}
	if err := runWSS(*httpsBase); err != nil {
		log.Fatalf("wss test failed: %v", err)
	}
	if err := runGRPC(*grpcAddr); err != nil {
		log.Fatalf("grpc test failed: %v", err)
	}
	log.Println("protocol target smoke complete")
}

func runHTTPS(base string) error {
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, // local deterministic target
		},
	}
	body := map[string]string{
		"message": "HTTPS outbound sensitive test: My SSN is 123-45-6789",
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/echo", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(resp.Body)
	log.Printf("HTTPS /echo status=%d body=%s", resp.StatusCode, string(reply))
	return nil
}

func runWSS(base string) error {
	wsURL := "wss" + base[len("https"):] + "/ws"
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, // local deterministic target
		HandshakeTimeout: 20 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	payload := "WSS outbound sensitive test: My SSN is 123-45-6789"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		return err
	}
	_, reply, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	log.Printf("WSS /ws reply=%s", string(reply))
	return nil
}

func runGRPC(addr string) error {
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}) // local deterministic target
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()

	client := grpc_health_v1.NewHealthClient(conn)
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "grpc outbound sensitive test 123-45-6789",
	})
	if err != nil {
		return err
	}
	log.Printf("gRPC health status=%s", resp.Status.String())
	return nil
}
