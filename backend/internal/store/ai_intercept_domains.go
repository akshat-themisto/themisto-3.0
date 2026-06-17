package store

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AIInterceptDomain stores one managed host allowlist entry for HTTPS interception.
type AIInterceptDomain struct {
	ID        int64     `json:"id"`
	OrgID     string    `json:"org_id"`
	Domain    string    `json:"domain"`
	Enabled   bool      `json:"enabled"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListAIInterceptDomains returns org-managed interception domains.
func (s *Store) ListAIInterceptDomains(ctx context.Context, orgID string) ([]AIInterceptDomain, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id::text, domain, enabled, source, created_at, updated_at
		FROM ai_intercept_domains
		WHERE org_id = $1::uuid
		ORDER BY domain ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AIInterceptDomain, 0)
	for rows.Next() {
		var row AIInterceptDomain
		if err := rows.Scan(&row.ID, &row.OrgID, &row.Domain, &row.Enabled, &row.Source, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListEffectiveAIInterceptDomains returns enabled interception domains for policy sync.
// If none are configured yet, it returns a deterministic baseline managed set.
func (s *Store) ListEffectiveAIInterceptDomains(ctx context.Context, orgID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT domain
		FROM ai_intercept_domains
		WHERE org_id = $1::uuid AND enabled = true
		ORDER BY domain ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	domains := make([]string, 0)
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(domains) == 0 {
		return defaultManagedAIInterceptDomains(), nil
	}
	return normalizeAndExpandInterceptDomains(domains), nil
}

// ReplaceManagedAIInterceptDomains sets the org's managed interception domains.
func (s *Store) ReplaceManagedAIInterceptDomains(ctx context.Context, orgID string, domains []string) ([]string, error) {
	if err := ValidateInterceptDomains(domains); err != nil {
		return nil, err
	}
	normalized := normalizeAndExpandInterceptDomains(domains)
	if len(normalized) == 0 {
		normalized = defaultManagedAIInterceptDomains()
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM ai_intercept_domains
		WHERE org_id = $1::uuid AND source = 'managed'`, orgID); err != nil {
		return nil, err
	}

	for _, domain := range normalized {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ai_intercept_domains (org_id, domain, enabled, source)
			VALUES ($1::uuid, $2, true, 'managed')
			ON CONFLICT (org_id, domain)
			DO UPDATE SET enabled = EXCLUDED.enabled, source = EXCLUDED.source, updated_at = now()`,
			orgID, domain,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return normalized, nil
}

// EnsureDefaultAIInterceptDomains seeds managed defaults for all orgs when missing.
func (s *Store) EnsureDefaultAIInterceptDomains(ctx context.Context, orgID string) error {
	var exists bool
	if err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM ai_intercept_domains WHERE org_id = $1::uuid
		)`, orgID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := s.ReplaceManagedAIInterceptDomains(ctx, orgID, defaultManagedAIInterceptDomains())
	return err
}

// EnsureDefaultAIInterceptDomainsForAllOrgs seeds defaults where missing for active orgs.
func (s *Store) EnsureDefaultAIInterceptDomainsForAllOrgs(ctx context.Context) error {
	orgs, err := s.GetOrganizationByAPIKeyHash(ctx)
	if err != nil {
		return err
	}
	for _, org := range orgs {
		if err := s.EnsureDefaultAIInterceptDomains(ctx, org.ID); err != nil {
			return fmt.Errorf("org %s: %w", org.ID, err)
		}
	}
	return nil
}

func normalizeAndExpandInterceptDomains(domains []string) []string {
	set := make(map[string]struct{}, len(domains)+16)
	for _, raw := range domains {
		n := normalizeInterceptDomain(raw)
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}

	for domain := range set {
		for _, alias := range vendorAliasDomains(domain) {
			if n := normalizeInterceptDomain(alias); n != "" {
				set[n] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func normalizeInterceptDomain(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "*.")
	if strings.Contains(v, "://") {
		if parsed, err := url.Parse(v); err == nil {
			v = parsed.Host
		}
	}
	if strings.Contains(v, "/") {
		parts := strings.SplitN(v, "/", 2)
		v = parts[0]
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimPrefix(v, ".")
	if v == "" {
		return ""
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			continue
		}
		return ""
	}
	if !strings.Contains(v, ".") {
		return ""
	}
	return v
}

func defaultManagedAIInterceptDomains() []string {
	return []string{
		"api.anthropic.com",
		"api.githubcopilot.com",
		"api.openai.com",
		"api2.cursor.sh",
		"copilot-proxy.githubusercontent.com",
		"content-gemini.googleapis.com",
		"generativelanguage.googleapis.com",
	}
}

func vendorAliasDomains(domain string) []string {
	switch {
	case domain == "api.openai.com":
		return []string{"api.openai.com"}
	case domain == "openai.com", domain == "chat.openai.com", domain == "chatgpt.com":
		return []string{"api.openai.com"}
	case strings.HasSuffix(domain, "anthropic.com") || domain == "claude.ai":
		return []string{"api.anthropic.com"}
	case strings.HasSuffix(domain, "google.com") || strings.HasSuffix(domain, "googleapis.com") || strings.Contains(domain, "gemini"):
		return []string{"generativelanguage.googleapis.com", "content-gemini.googleapis.com"}
	case strings.HasSuffix(domain, "githubcopilot.com") || strings.HasSuffix(domain, "githubusercontent.com"):
		return []string{"api.githubcopilot.com", "copilot-proxy.githubusercontent.com"}
	case strings.Contains(domain, "cursor"):
		return []string{"api2.cursor.sh"}
	default:
		return nil
	}
}

// ValidateInterceptDomains verifies that all provided values are valid host domains.
func ValidateInterceptDomains(domains []string) error {
	for _, raw := range domains {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if normalizeInterceptDomain(raw) == "" {
			return fmt.Errorf("invalid domain: %s", strings.TrimSpace(raw))
		}
	}
	return nil
}
