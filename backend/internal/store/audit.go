package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type AuditEntry struct {
	ActorType    string
	ActorID      string
	OrgID        *string
	Action       string
	ResourceType string
	ResourceID   string
	Details      map[string]interface{}
	IPAddress    *string
}

func (s *Store) InsertAudit(ctx context.Context, tx *sql.Tx, entry *AuditEntry) error {
	var detailsJSON []byte
	var err error
	if entry.Details != nil {
		detailsJSON, err = json.Marshal(entry.Details)
		if err != nil {
			return err
		}
	}

	execer := dbExecer(tx, s.DB)
	_, err = execer.ExecContext(ctx,
		`INSERT INTO audit_log (actor_type, actor_id, org_id, action, resource_type, resource_id, details, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ActorType, entry.ActorID, entry.OrgID, entry.Action,
		entry.ResourceType, entry.ResourceID, detailsJSON, entry.IPAddress)
	return err
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func dbExecer(tx *sql.Tx, db *sql.DB) execContext {
	if tx != nil {
		return tx
	}
	return db
}
