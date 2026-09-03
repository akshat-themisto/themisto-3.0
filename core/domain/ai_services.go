package domain

import (
	_ "embed"
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strings"
)

// AIProductCatalogEntry is the endpoint-facing, data-driven description of an
// AI product. Built-in entries are compiled into the agent and policy entries
// may extend (but not silently rewrite) them at runtime.
type AIProductCatalogEntry struct {
	VendorKey          string   `json:"vendor_key"`
	ProductKey         string   `json:"product_key"`
	DisplayName        string   `json:"display_name"`
	FunctionalCategory string   `json:"functional_category"`
	Domains            []string `json:"domains,omitempty"`
	ProcessNames       []string `json:"process_names,omitempty"`
	BundleIDs          []string `json:"bundle_ids,omitempty"`
	Surfaces           []string `json:"surfaces,omitempty"`
	MCPConfigKeys      []string `json:"mcp_config_keys,omitempty"`
	APIOnly            bool     `json:"api_only,omitempty"`
}

// AIServiceInfo is retained as the compatibility view used by routing and
// policy. Vendor-specific facts originate in the catalog, not ledger code.
type AIServiceInfo struct {
	Vendor     string
	Product    string
	Name       string
	Category   string
	RiskTier   string
	Sanctioned bool
	Surfaces   []string
}

//go:embed ai_products.json
var builtInAIProductsJSON []byte

var builtInAIProducts = mustLoadAIProductCatalog(builtInAIProductsJSON)

// BuiltInAIProductCatalog returns an isolated copy of the compiled endpoint
// catalog so callers cannot mutate global detection behavior.
func BuiltInAIProductCatalog() []AIProductCatalogEntry {
	return cloneAIProductCatalog(builtInAIProducts)
}

// MergeAIProductCatalog appends valid policy-delivered products. Existing
// vendor/product keys remain authoritative to prevent a policy from changing
// the meaning of an already deployed product key.
func MergeAIProductCatalog(base, additions []AIProductCatalogEntry) []AIProductCatalogEntry {
	out := normalizeAIProductCatalog(base)
	seen := make(map[string]struct{}, len(out))
	for _, entry := range out {
		seen[entry.VendorKey+"/"+entry.ProductKey] = struct{}{}
	}
	for _, entry := range normalizeAIProductCatalog(additions) {
		key := entry.VendorKey + "/" + entry.ProductKey
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	return out
}

// LookupAIProduct evaluates host and optional process metadata against a
// supplied catalog. Exact domain matches take precedence over suffix matches.
func LookupAIProduct(catalog []AIProductCatalogEntry, host string, process ProcessInfo) (AIProductCatalogEntry, bool) {
	host = NormalizeHost(host)
	processName := normalizeCatalogValue(process.Name)
	bundleID := normalizeCatalogValue(process.BundleID)
	for _, entry := range catalog {
		for _, domain := range entry.Domains {
			if host != "" && host == NormalizeHost(domain) {
				return entry, true
			}
		}
	}
	for _, entry := range catalog {
		for _, domain := range entry.Domains {
			domain = NormalizeHost(domain)
			if host != "" && domain != "" && strings.HasSuffix(host, "."+domain) {
				return entry, true
			}
		}
		if containsCatalogValue(entry.BundleIDs, bundleID) {
			return entry, true
		}
	}
	for _, entry := range catalog {
		if len(entry.BundleIDs) == 0 && containsCatalogValue(entry.ProcessNames, processName) {
			return entry, true
		}
	}
	for _, entry := range catalog {
		if containsCatalogValue(entry.ProcessNames, processName) {
			return entry, true
		}
	}
	return AIProductCatalogEntry{}, false
}

// LookupAIProductByKey returns the catalog entry for an exact stable product
// key. It is used by endpoint discovery where a process or local
// configuration provides the observation instead of a network hostname.
func LookupAIProductByKey(catalog []AIProductCatalogEntry, productKey string) (AIProductCatalogEntry, bool) {
	productKey = normalizeCatalogValue(productKey)
	for _, entry := range catalog {
		if normalizeCatalogValue(entry.ProductKey) == productKey {
			return entry, true
		}
	}
	return AIProductCatalogEntry{}, false
}

// NormalizeHost canonicalizes host-like values coming from request targets,
// Origin / Referer headers, or CONNECT metadata.
func NormalizeHost(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			raw = parsed.Host
		}
	}
	if host, port, err := net.SplitHostPort(raw); err == nil && port != "" {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	return strings.TrimSuffix(raw, ".")
}

// LookupAIService uses the built-in catalog for compatibility with existing
// routing callers.
func LookupAIService(host string) (AIServiceInfo, bool) {
	entry, ok := LookupAIProduct(builtInAIProducts, host, ProcessInfo{})
	if !ok {
		return AIServiceInfo{}, false
	}
	return serviceInfo(entry), true
}

// InferAIService expands host-only matching with Origin / Referer hints for
// helper domains. A signal only promotes traffic when both hosts are owned by
// the same catalog product/vendor domain family.
func InferAIService(host string, signals ...string) (AIServiceInfo, bool) {
	if info, ok := LookupAIService(host); ok {
		return info, true
	}
	host = NormalizeHost(host)
	if host == "" {
		return AIServiceInfo{}, false
	}
	for _, signal := range signals {
		entry, ok := LookupAIProduct(builtInAIProducts, NormalizeHost(signal), ProcessInfo{})
		if !ok || !catalogEntryOwnsHost(entry, host) {
			continue
		}
		return serviceInfo(entry), true
	}
	return AIServiceInfo{}, false
}

func catalogEntryOwnsHost(entry AIProductCatalogEntry, host string) bool {
	host = NormalizeHost(host)
	for _, domain := range entry.Domains {
		domain = NormalizeHost(domain)
		if registrableDomainFamily(host) == registrableDomainFamily(domain) {
			return true
		}
	}
	return false
}

func registrableDomainFamily(host string) string {
	parts := strings.Split(NormalizeHost(host), ".")
	if len(parts) < 2 {
		return NormalizeHost(host)
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func serviceInfo(entry AIProductCatalogEntry) AIServiceInfo {
	return AIServiceInfo{
		Vendor:   entry.VendorKey,
		Product:  entry.ProductKey,
		Name:     entry.DisplayName,
		Category: legacyServiceCategory(entry.FunctionalCategory),
		Surfaces: append([]string(nil), entry.Surfaces...),
	}
}

func legacyServiceCategory(functionalCategory string) string {
	switch functionalCategory {
	case "coding_agent":
		return "ai_code"
	case "image_generation":
		return "ai_image"
	default:
		return "ai_llm"
	}
}

func mustLoadAIProductCatalog(raw []byte) []AIProductCatalogEntry {
	var entries []AIProductCatalogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		panic("invalid embedded AI product catalog: " + err.Error())
	}
	return normalizeAIProductCatalog(entries)
}

func normalizeAIProductCatalog(entries []AIProductCatalogEntry) []AIProductCatalogEntry {
	out := make([]AIProductCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.VendorKey = normalizeCatalogValue(entry.VendorKey)
		entry.ProductKey = normalizeCatalogValue(entry.ProductKey)
		entry.FunctionalCategory = normalizeCatalogValue(entry.FunctionalCategory)
		entry.DisplayName = strings.TrimSpace(entry.DisplayName)
		if entry.VendorKey == "" || entry.ProductKey == "" || entry.DisplayName == "" {
			continue
		}
		if entry.FunctionalCategory == "" {
			entry.FunctionalCategory = "unknown"
		}
		entry.Domains = normalizeCatalogValues(entry.Domains, true)
		entry.ProcessNames = normalizeCatalogValues(entry.ProcessNames, false)
		entry.BundleIDs = normalizeCatalogValues(entry.BundleIDs, false)
		entry.Surfaces = normalizeCatalogValues(entry.Surfaces, false)
		entry.MCPConfigKeys = normalizeCatalogValues(entry.MCPConfigKeys, false)
		out = append(out, entry)
	}
	return out
}

func cloneAIProductCatalog(entries []AIProductCatalogEntry) []AIProductCatalogEntry {
	out := make([]AIProductCatalogEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].Domains = append([]string(nil), entry.Domains...)
		out[i].ProcessNames = append([]string(nil), entry.ProcessNames...)
		out[i].BundleIDs = append([]string(nil), entry.BundleIDs...)
		out[i].Surfaces = append([]string(nil), entry.Surfaces...)
		out[i].MCPConfigKeys = append([]string(nil), entry.MCPConfigKeys...)
	}
	return out
}

func normalizeCatalogValues(values []string, host bool) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if host {
			value = NormalizeHost(value)
		} else {
			value = normalizeCatalogValue(value)
		}
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeCatalogValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsCatalogValue(values []string, target string) bool {
	if target == "" {
		return false
	}
	for _, value := range values {
		if normalizeCatalogValue(value) == target {
			return true
		}
	}
	return false
}
