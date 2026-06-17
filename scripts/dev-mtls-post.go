//go:build ignore

package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	url := flag.String("url", "", "URL to POST")
	certPath := flag.String("cert", "", "client certificate PEM path")
	keyPath := flag.String("key", "", "client private key PEM path")
	caPath := flag.String("ca", "", "CA chain PEM path")
	bodyPath := flag.String("body", "", "JSON body file path")
	flag.Parse()

	if *url == "" || *certPath == "" || *keyPath == "" || *caPath == "" || *bodyPath == "" {
		fmt.Fprintln(os.Stderr, "missing required flags: -url -cert -key -ca -body")
		os.Exit(2)
	}

	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		fatal("load client cert", err)
	}
	caPEM, err := os.ReadFile(*caPath)
	if err != nil {
		fatal("read ca", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		fmt.Fprintln(os.Stderr, "failed to parse CA chain")
		os.Exit(1)
	}
	body, err := os.Open(*bodyPath)
	if err != nil {
		fatal("open body", err)
	}
	defer body.Close()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      roots,
				MinVersion:   tls.VersionTLS12,
			},
		},
	}
	resp, err := client.Post(*url, "application/json", body)
	if err != nil {
		fatal("post", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, string(out))
	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}

func fatal(label string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
	os.Exit(1)
}
