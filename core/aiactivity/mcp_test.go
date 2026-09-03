package aiactivity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverMacOSMCPReturnsAggregateOnly(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `{"mcpServers":{"private-server":{"command":"/Users/person/secret-tool","args":["--token","secret"]},"remote":{"url":"https://private.example/mcp"}}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	observations, err := DiscoverMacOSMCP(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].ClientKey != "cursor" || observations[0].ServerCount != 2 {
		t.Fatalf("unexpected aggregate: %#v", observations)
	}
	encoded, _ := json.Marshal(observations)
	for _, forbidden := range []string{"private-server", "/Users/person", "secret-tool", "--token", "private.example"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("MCP discovery leaked %q: %s", forbidden, encoded)
		}
	}
}
