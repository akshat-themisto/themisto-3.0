package store

import "context"

// EnsurePolicyControlsSchema keeps gateway policy sync compatible with
// databases created before org-level prompt enforcement overrides existed.
func (s *Store) EnsurePolicyControlsSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		ALTER TABLE organizations
		    ADD COLUMN IF NOT EXISTS prompt_enforcement_override TEXT NOT NULL DEFAULT '';`)
	return err
}
