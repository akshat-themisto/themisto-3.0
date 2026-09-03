//go:build darwin

package darwin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/themisto/agent/core/aiactivity"
	"github.com/themisto/agent/core/domain"
)

type processSnapshot struct {
	pid      int
	name     string
	bundleID string
}

// DiscoverEndpointAI samples running catalog products and known MCP client
// configurations. Only aggregate catalog keys leave this method; executable
// paths, OS usernames, commands, arguments, and MCP server details do not.
func (a *Adapter) DiscoverEndpointAI(ctx context.Context, catalog []domain.AIProductCatalogEntry) ([]domain.EndpointAIObservation, error) {
	processes, processErr := listProcessSnapshots(ctx)
	observations := matchProcessObservations(catalog, processes)

	if home, err := loggedInUserHome(ctx); err == nil {
		if mcp, err := aiactivity.DiscoverMacOSMCP(home); err == nil {
			observations = append(observations, matchMCPObservations(catalog, mcp)...)
		} else {
			a.darwinProcessResolver.log.Warn("MCP discovery unavailable", "error", err)
		}
	}
	if processErr != nil && len(observations) == 0 {
		return nil, processErr
	}
	return observations, nil
}

func listProcessSnapshots(ctx context.Context) ([]processSnapshot, error) {
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	var processes []processSnapshot
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		processes = append(processes, processSnapshot{
			pid:      pid,
			name:     strings.ToLower(filepath.Base(command)),
			bundleID: strings.ToLower(resolveBundleID(command)),
		})
	}
	return processes, scanner.Err()
}

func matchProcessObservations(catalog []domain.AIProductCatalogEntry, processes []processSnapshot) []domain.EndpointAIObservation {
	seen := make(map[string]struct{})
	var out []domain.EndpointAIObservation
	for _, process := range processes {
		product, matched := domain.LookupAIProduct(catalog, "", domain.ProcessInfo{
			Name: process.name, BundleID: process.bundleID,
		})
		if !matched {
			continue
		}
		if product.APIOnly {
			continue
		}
		key := product.VendorKey + "/" + product.ProductKey
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, domain.EndpointAIObservation{
			VendorKey:         product.VendorKey,
			ProductKey:        product.ProductKey,
			Surface:           processSurface(product.Surfaces),
			ActivityKind:      "process_observed",
			SourceApplication: process.name,
			Count:             1,
		})
	}
	return out
}

func matchesProcess(product domain.AIProductCatalogEntry, process processSnapshot) bool {
	for _, bundleID := range product.BundleIDs {
		if strings.EqualFold(bundleID, process.bundleID) && process.bundleID != "" {
			return true
		}
	}
	for _, name := range product.ProcessNames {
		if strings.EqualFold(name, process.name) {
			return true
		}
	}
	return false
}

func processSurface(surfaces []string) string {
	for _, preferred := range []string{"desktop", "coding_agent", "local_model", "terminal"} {
		for _, surface := range surfaces {
			if surface == preferred {
				return surface
			}
		}
	}
	if len(surfaces) > 0 {
		return surfaces[0]
	}
	return "process"
}

func matchMCPObservations(catalog []domain.AIProductCatalogEntry, observations []aiactivity.MCPObservation) []domain.EndpointAIObservation {
	var out []domain.EndpointAIObservation
	for _, observation := range observations {
		for _, product := range catalog {
			if !containsFold(product.MCPConfigKeys, observation.ClientKey) {
				continue
			}
			count := int64(observation.ServerCount)
			if count < 1 {
				count = 1
			}
			out = append(out, domain.EndpointAIObservation{
				VendorKey:         product.VendorKey,
				ProductKey:        product.ProductKey,
				Surface:           "mcp_client",
				ActivityKind:      "mcp_configured",
				SourceApplication: observation.ClientKey,
				Count:             count,
			})
			break
		}
	}
	return out
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func loggedInUserHome(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "stat", "-f", "%Su", "/dev/console").Output()
	if err != nil {
		return "", fmt.Errorf("discover console user: %w", err)
	}
	username := strings.TrimSpace(string(out))
	if username == "" || username == "root" || username == "loginwindow" {
		return "", fmt.Errorf("no logged-in user session")
	}
	out, err = exec.CommandContext(ctx, "dscl", ".", "-read", "/Users/"+username, "NFSHomeDirectory").Output()
	if err != nil {
		return "", fmt.Errorf("discover console user home: %w", err)
	}
	const prefix = "NFSHomeDirectory:"
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), prefix))
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("console user has no absolute home")
	}
	if _, err := os.Stat(value); err != nil {
		return "", fmt.Errorf("console user home unavailable: %w", err)
	}
	return value, nil
}
