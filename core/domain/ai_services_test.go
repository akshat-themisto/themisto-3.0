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
