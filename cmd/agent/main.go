// Package main is the entrypoint for the Themisto agent. It wires the OS
// adapter, configuration source, and logger, then hands off to the core.
package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	agentcore "github.com/themisto/agent/core"
	"github.com/themisto/agent/pkg/log"
)

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to agent configuration file")
	certFile := flag.String("cert", "", "path to client certificate PEM file (optional, for file-based bootstrap)")
	keyFile := flag.String("key", "", "path to client private key PEM file (optional, for file-based bootstrap)")
	caFile := flag.String("ca", "", "path to CA certificate PEM file (optional, for file-based bootstrap)")
	activate := flag.Bool("activate", false, "activate device using enrollment token, write certs to disk, and update config")
	install := flag.Bool("install", false, "install Themisto agent (Windows: copies binary, config, certs, registers service)")
	installSourceDir := flag.String("install-source-dir", "", "internal: source directory for install (set automatically during UAC elevation)")
	uninstall := flag.Bool("uninstall", false, "uninstall Themisto agent and remove service")
	serviceStart := flag.Bool("service-start", false, "start the agent background service")
	serviceStop := flag.Bool("service-stop", false, "stop the agent background service")
	serviceStatus := flag.Bool("service-status", false, "show the agent service status")
	claudeCodeHook := flag.Bool("claude-code-hook", false, "internal: run the Claude Code UserPromptSubmit hook handler")
	copilotHook := flag.Bool("copilot-hook", false, "internal: run the GitHub Copilot hook handler")
	cursorHook := flag.Bool("cursor-hook", false, "internal: run the Cursor prompt hook handler")
	windsurfHook := flag.Bool("windsurf-hook", false, "internal: run the Windsurf pre_user_prompt hook handler")
	proxyOff := flag.Bool("proxy-off", false, "emergency: clear system proxy settings (use if internet stops working)")
	deviceID := flag.String("device-id", "", "device ID for activation (overrides config)")
	enrollmentToken := flag.String("enrollment-token", "", "one-time enrollment token for activation (overrides config)")
	backendURL := flag.String("backend-url", "", "backend enrollment base URL (overrides config)")
	gatewayURL := flag.String("gateway-url", "", "gateway URL for runtime traffic forwarding (overrides config)")
	orgName := flag.String("org-name", "", "organization name for CSR subject O= (overrides config)")
	outputDir := flag.String("output-dir", "", "directory for generated cert/key/ca files (default: <config-dir>/certs)")
	backendCA := flag.String("backend-ca", "", "optional CA bundle PEM for enrollment TLS verification")
	insecureEnrollmentTLS := flag.Bool("insecure-enrollment-tls", false, "skip TLS verification for enrollment requests (development only)")
	keepEnrollmentToken := flag.Bool("keep-enrollment-token", false, "keep enrollment token in config after successful activation")
	flag.Parse()

	// If launched by SCM, enter the service handler immediately.
	if isRunningAsService() {
		logger := &stdLogger{}
		if err := runAsService(*configPath); err != nil {
			logger.Error("service run failed", "error", err)
			os.Exit(1)
		}
		return
	}

	logger := &stdLogger{}

	// Installer / service management commands (platform-specific)
	if *install {
		if err := runInstall(*installSourceDir, logger); err != nil {
			logger.Error("install failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *uninstall {
		if err := runUninstall(logger); err != nil {
			logger.Error("uninstall failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *proxyOff {
		if err := runProxyOff(logger); err != nil {
			logger.Error("proxy-off failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *serviceStart {
		if err := runServiceStart(logger); err != nil {
			logger.Error("service start failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *serviceStop {
		if err := runServiceStop(logger); err != nil {
			logger.Error("service stop failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *serviceStatus {
		if err := runServiceStatus(logger); err != nil {
			logger.Error("service status failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if *claudeCodeHook {
		os.Exit(runClaudeCodeHook())
	}
	if *copilotHook {
		os.Exit(runCopilotHook())
	}
	if *cursorHook {
		os.Exit(runCursorHook())
	}
	if *windsurfHook {
		os.Exit(runWindsurfHook())
	}

	if *activate {
		if err := runActivation(activationOptions{
			ConfigPath:      *configPath,
			BackendURL:      *backendURL,
			GatewayURL:      *gatewayURL,
			DeviceID:        *deviceID,
			EnrollmentToken: *enrollmentToken,
			OrgName:         *orgName,
			OutputDir:       *outputDir,
			CACertPath:      *backendCA,
			InsecureTLS:     *insecureEnrollmentTLS,
			ClearToken:      !*keepEnrollmentToken,
		}, logger); err != nil {
			logger.Error("activation failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := maybeActivateFromConfig(*configPath, logger); err != nil {
		logger.Error("automatic activation failed", "error", err)
		os.Exit(1)
	}

	src := &fileSource{path: *configPath}

	adapter := newPlatformAdapter(logger)

	agent, err := agentcore.NewAgent(agentcore.Deps{
		Adapter: adapter,
		Source:  src,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("agent init failed", "error", err)
		os.Exit(1)
	}

	// If cert files are provided, bootstrap identity from files instead of Keychain
	if *certFile != "" && *keyFile != "" && *caFile != "" {
		logger.Info("bootstrapping identity from files", "cert", *certFile, "key", *keyFile, "ca", *caFile)

		certPEM, err := os.ReadFile(*certFile)
		if err != nil {
			logger.Error("read cert file failed", "error", err)
			os.Exit(1)
		}
		keyPEM, err := os.ReadFile(*keyFile)
		if err != nil {
			logger.Error("read key file failed", "error", err)
			os.Exit(1)
		}
		caPEM, err := os.ReadFile(*caFile)
		if err != nil {
			logger.Error("read CA file failed", "error", err)
			os.Exit(1)
		}

		certBlock, _ := pem.Decode(certPEM)
		if certBlock == nil {
			logger.Error("invalid cert PEM")
			os.Exit(1)
		}
		keyBlock, _ := pem.Decode(keyPEM)
		if keyBlock == nil {
			logger.Error("invalid key PEM")
			os.Exit(1)
		}
		caBlock, _ := pem.Decode(caPEM)
		if caBlock == nil {
			logger.Error("invalid CA PEM")
			os.Exit(1)
		}

		// Parse cert to extract serial for caID
		cert, err := x509.ParseCertificate(certBlock.Bytes)
		if err != nil {
			logger.Error("parse cert failed", "error", err)
			os.Exit(1)
		}

		caID := "file-ca"
		clientID := cert.Subject.CommonName

		agent.BootstrapIdentity(caID, clientID, certBlock.Bytes, keyBlock.Bytes, caBlock.Bytes)
		logger.Info("identity bootstrapped from files", "client_id", clientID)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		logger.Error("agent exited with error", "error", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// fileSource implements config.Source
// ---------------------------------------------------------------------------

type fileSource struct {
	path string
}

func (f *fileSource) ConfigBytes() ([]byte, string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, f.path, fmt.Errorf("read config: %w", err)
	}
	return data, f.path, nil
}

// Path implements config.PathSource.
func (f *fileSource) Path() string {
	return f.path
}

// ---------------------------------------------------------------------------
// stdLogger implements pkg/log.Logger using the standard library
// ---------------------------------------------------------------------------

type stdLogger struct {
	prefix []interface{}
}

func (l *stdLogger) Debug(msg string, keyvals ...interface{}) { l.emit("DEBUG", msg, keyvals) }
func (l *stdLogger) Info(msg string, keyvals ...interface{})  { l.emit("INFO", msg, keyvals) }
func (l *stdLogger) Warn(msg string, keyvals ...interface{})  { l.emit("WARN", msg, keyvals) }
func (l *stdLogger) Error(msg string, keyvals ...interface{}) { l.emit("ERROR", msg, keyvals) }

func (l *stdLogger) With(keyvals ...interface{}) log.Logger {
	combined := make([]interface{}, 0, len(l.prefix)+len(keyvals))
	combined = append(combined, l.prefix...)
	combined = append(combined, keyvals...)
	return &stdLogger{prefix: combined}
}

func (l *stdLogger) emit(level, msg string, keyvals []interface{}) {
	fmt.Fprintf(os.Stderr, "level=%s msg=%q", level, msg)
	all := append(l.prefix, keyvals...)
	for i := 0; i+1 < len(all); i += 2 {
		fmt.Fprintf(os.Stderr, " %v=%v", all[i], all[i+1])
	}
	fmt.Fprintln(os.Stderr)
}
