package aiactivity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// MCPObservation is deliberately aggregate-only. Server names, paths,
// commands, arguments, environment variables, and payloads never leave the
// endpoint.
type MCPObservation struct {
	ClientKey      string   `json:"client_key"`
	Configured     bool     `json:"configured"`
	ServerCount    int      `json:"server_count"`
	TransportKinds []string `json:"transport_kinds,omitempty"`
}

type mcpLocation struct {
	clientKey string
	relative  string
}

var macOSMCPLocations = []mcpLocation{
	{clientKey: "claude_desktop", relative: "Library/Application Support/Claude/claude_desktop_config.json"},
	{clientKey: "claude_code", relative: ".claude.json"},
	{clientKey: "cursor", relative: ".cursor/mcp.json"},
	{clientKey: "cursor", relative: "Library/Application Support/Cursor/User/mcp.json"},
	{clientKey: "windsurf", relative: ".codeium/windsurf/mcp_config.json"},
	{clientKey: "vscode", relative: "Library/Application Support/Code/User/mcp.json"},
}

// DiscoverMacOSMCP reads only known per-user configuration files below the
// explicitly supplied home. Callers must pass the logged-in user's home; this
// function never treats the OS username as corporate identity.
func DiscoverMacOSMCP(userHome string) ([]MCPObservation, error) {
	if userHome == "" || !filepath.IsAbs(userHome) {
		return nil, errors.New("explicit absolute user home is required")
	}
	byClient := map[string]MCPObservation{}
	for _, location := range macOSMCPLocations {
		path := filepath.Join(userHome, filepath.FromSlash(location.relative))
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		count, transports := summarizeMCPConfig(raw)
		current := byClient[location.clientKey]
		current.ClientKey = location.clientKey
		current.Configured = true
		current.ServerCount += count
		current.TransportKinds = mergeStrings(current.TransportKinds, transports)
		byClient[location.clientKey] = current
	}
	out := make([]MCPObservation, 0, len(byClient))
	for _, observation := range byClient {
		out = append(out, observation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClientKey < out[j].ClientKey })
	return out, nil
}

func summarizeMCPConfig(raw []byte) (int, []string) {
	var document map[string]interface{}
	if json.Unmarshal(raw, &document) != nil {
		return 0, nil
	}
	servers := objectAt(document, "mcpServers")
	if servers == nil {
		servers = objectAt(document, "servers")
	}
	transports := map[string]struct{}{}
	for _, value := range servers {
		server, _ := value.(map[string]interface{})
		switch {
		case server["url"] != nil:
			transports["http"] = struct{}{}
		case server["command"] != nil:
			transports["stdio"] = struct{}{}
		default:
			transports["unknown"] = struct{}{}
		}
	}
	out := make([]string, 0, len(transports))
	for transport := range transports {
		out = append(out, transport)
	}
	sort.Strings(out)
	return len(servers), out
}

func objectAt(document map[string]interface{}, key string) map[string]interface{} {
	value, _ := document[key].(map[string]interface{})
	return value
}

func mergeStrings(left, right []string) []string {
	set := make(map[string]struct{}, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
