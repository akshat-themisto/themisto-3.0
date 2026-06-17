package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	wsUpgrader = websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
)

func main() {
	var (
		httpsAddr = flag.String("https_addr", "127.0.0.1:18443", "HTTPS server address for HTTP echo + WSS")
		grpcAddr  = flag.String("grpc_addr", "127.0.0.1:18444", "gRPC TLS server address")
		certDir   = flag.String("cert_dir", "/tmp/themisto-protocol-targets", "Directory for generated TLS certs")
	)
	flag.Parse()

	certFile, keyFile, err := ensureSelfSignedCert(*certDir)
	if err != nil {
		log.Fatalf("create TLS certificate: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              *httpsAddr,
		Handler:           buildHTTPMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Printf("HTTPS target listening on https://%s", *httpsAddr)
		if err := httpSrv.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("https server failed: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := runGRPCServer(*grpcAddr, certFile, keyFile); err != nil {
			log.Fatalf("grpc server failed: %v", err)
		}
	}()

	wg.Wait()
}

func buildHTTPMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /browser", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(browserHarnessHTML))
	})
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBounded(r, 2<<20)
		resp := map[string]interface{}{
			"method":      r.Method,
			"path":        r.URL.Path,
			"contentType": r.Header.Get("Content-Type"),
			"length":      len(body),
			"body":        string(body),
			"time":        time.Now().UTC().Format(time.RFC3339Nano),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, payload); err != nil {
				return
			}
		}
	})
	return mux
}

func runGRPCServer(addr, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load TLS keypair: %w", err)
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}
	defer lis.Close()

	creds := credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	})
	srv := grpc.NewServer(grpc.Creds(creds))

	healthSvc := health.NewServer()
	healthSvc.SetServingStatus("themisto.protocol.targets", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(srv, healthSvc)

	log.Printf("gRPC target listening on %s", addr)
	return srv.Serve(lis)
}

func readBounded(r *http.Request, max int64) ([]byte, error) {
	defer r.Body.Close()
	return ioReadAllLimit(r.Body, max)
}

func ioReadAllLimit(r io.Reader, max int64) ([]byte, error) {
	lr := &io.LimitedReader{R: r, N: max + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return data[:max], nil
	}
	return data, nil
}

func ensureSelfSignedCert(dir string) (certPath string, keyPath string, _ error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")

	if _, certErr := os.Stat(certPath); certErr == nil {
		if _, keyErr := os.Stat(keyPath); keyErr == nil {
			return certPath, keyPath, nil
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}

	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "themisto-protocol-targets.local",
			Organization: []string{"Themisto"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

const browserHarnessHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Themisto Protocol Targets</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 24px; }
    h1 { margin-top: 0; }
    textarea, input, button { font-size: 14px; }
    textarea { width: 100%; max-width: 760px; height: 110px; }
    .row { margin-bottom: 20px; }
    pre { background: #111; color: #f5f5f5; padding: 12px; border-radius: 8px; max-width: 760px; overflow: auto; }
  </style>
</head>
<body>
  <h1>Themisto HTTPS/WSS Test Harness</h1>
  <div class="row">
    <h3>HTTPS /echo</h3>
    <textarea id="payload">My SSN is 123-45-6789 and token sk-example-secret.</textarea><br>
    <button onclick="sendEcho()">Send HTTPS Request</button>
  </div>
  <div class="row">
    <h3>WSS /ws</h3>
    <input id="wsPayload" value="websocket outbound My SSN is 123-45-6789" size="70">
    <button onclick="sendWS()">Send WSS Message</button>
  </div>
  <pre id="out">Ready.</pre>
  <script>
    async function sendEcho() {
      const payload = document.getElementById("payload").value;
      const res = await fetch("/echo", {
        method: "POST",
        headers: {"Content-Type":"application/json"},
        body: JSON.stringify({message: payload})
      });
      const txt = await res.text();
      document.getElementById("out").textContent = "HTTPS status=" + res.status + "\n" + txt;
    }
    function sendWS() {
      const payload = document.getElementById("wsPayload").value;
      const ws = new WebSocket((location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws");
      ws.onopen = () => ws.send(payload);
      ws.onmessage = (evt) => {
        document.getElementById("out").textContent = "WSS echo:\n" + evt.data;
        ws.close();
      };
      ws.onerror = (e) => {
        document.getElementById("out").textContent = "WSS error: " + e;
      };
    }
  </script>
</body>
</html>`
