package domain

import "testing"

func TestLookupAIService_NormalizesPortAndSuffix(t *testing.T) {
	info, ok := LookupAIService("chatgpt.com:443")
	if !ok {
		t.Fatal("expected chatgpt.com:443 to be recognized")
	}
	if info.Vendor != "openai" {
		t.Fatalf("vendor = %q, want openai", info.Vendor)
	}

	info, ok = LookupAIService("alkalimakersuite-pa.clients6.google.com")
	if !ok {
		t.Fatal("expected Google helper subdomain to be recognized")
	}
	if info.Vendor != "google" {
		t.Fatalf("vendor = %q, want google", info.Vendor)
	}
}

func TestBuiltInCatalogCoversMacOSProductsAndLocalModels(t *testing.T) {
	cases := []ProcessInfo{
		{Name: "ChatGPT", BundleID: "com.openai.chat"},
		{Name: "Claude", BundleID: "com.anthropic.claudefordesktop"},
		{Name: "Cursor", BundleID: "com.todesktop.230313mzl4w4u92"},
		{Name: "Windsurf", BundleID: "com.exafunction.windsurf"},
		{Name: "Ollama"},
		{Name: "LM Studio"},
		{Name: "Antigravity", BundleID: "com.google.antigravity"},
	}
	for _, process := range cases {
		if _, ok := LookupAIProduct(BuiltInAIProductCatalog(), "", process); !ok {
			t.Errorf("process not detected: %+v", process)
		}
	}
}

func TestPolicyCatalogExtendsWithoutOverwritingBuiltIn(t *testing.T) {
	base := BuiltInAIProductCatalog()
	merged := MergeAIProductCatalog(base, []AIProductCatalogEntry{
		{VendorKey: "acme", ProductKey: "assistant", DisplayName: "Acme Assistant", FunctionalCategory: "general_assistant", Domains: []string{"ai.acme.test"}},
		{VendorKey: "openai", ProductKey: "chatgpt", DisplayName: "Rewritten", FunctionalCategory: "unknown"},
	})
	entry, ok := LookupAIProduct(merged, "ai.acme.test", ProcessInfo{})
	if !ok || entry.ProductKey != "assistant" {
		t.Fatalf("policy addition not detected: %+v", entry)
	}
	builtin, ok := LookupAIProduct(merged, "chatgpt.com", ProcessInfo{})
	if !ok || builtin.DisplayName == "Rewritten" {
		t.Fatalf("built-in entry was overwritten: %+v", builtin)
	}
}

func TestInferAIService_UsesOriginForVendorOwnedHelperHost(t *testing.T) {
	info, ok := InferAIService("clients6.google.com", "https://gemini.google.com/app")
	if !ok {
		t.Fatal("expected helper host to be inferred from Gemini origin")
	}
	if info.Vendor != "google" {
		t.Fatalf("vendor = %q, want google", info.Vendor)
	}
	if info.Category != "ai_llm" {
		t.Fatalf("category = %q, want ai_llm", info.Category)
	}
}

func TestInferAIService_DoesNotPromoteUnrelatedTraffic(t *testing.T) {
	if _, ok := InferAIService("accounts.google.com", "https://mail.google.com/"); ok {
		t.Fatal("did not expect unrelated Google traffic to be promoted to AI")
	}
}
