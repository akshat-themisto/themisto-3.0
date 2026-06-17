// Command unified runs the merged agent+gateway development service.
//
// Usage:
//
//	go run ./cmd/unified
//	go run ./cmd/unified -listen 127.0.0.1:9090
//	go run ./cmd/unified -policy policy.json
//
// Then configure your browser or tool to use the HTTP proxy:
//
//	curl --proxy http://127.0.0.1:8080 http://example.com
//	curl --proxy http://127.0.0.1:8080 https://example.com
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/themisto/agent/unified"
)

func main() {
	listenAddr := flag.String("listen", "", "proxy listen address (default 127.0.0.1:8080)")
	configPath := flag.String("config", "", "path to dev config JSON (optional)")
	policyPath := flag.String("policy", "", "path to policy JSON (optional)")
	logOutput := flag.String("log", "stdout", "log output: stdout or file path")
	logJSON := flag.Bool("json", true, "emit structured JSON logs")
	flag.Parse()

	cfg, err := unified.LoadDevConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// CLI flags override config file.
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if *policyPath != "" {
		cfg.PolicyFile = *policyPath
	}
	if *logOutput != "" {
		cfg.LogOutput = *logOutput
	}
	cfg.LogJSON = *logJSON

	svc, err := unified.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := svc.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
