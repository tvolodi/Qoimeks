package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Feedback is one row of the append-only feedback table.
type Feedback struct {
	ID             int64           `json:"id"`
	CreatedAt      time.Time       `json:"created_at"`
	Kind           string          `json:"kind"`
	Text           string          `json:"text"`
	VoiceText      *string         `json:"voice_text"`
	AppVersion     string          `json:"app_version"`
	AndroidVersion string          `json:"android_version"`
	Device         string          `json:"device"`
	Operator       string          `json:"operator"`
	Screen         string          `json:"screen"`
	Errors         json.RawMessage `json:"errors"`
	Provider       *string         `json:"provider"`
}

// AppendFeedback stores f (ID and CreatedAt are ignored) and returns the new id.
func (s *Store) AppendFeedback(ctx context.Context, f Feedback) (int64, error) {
	if f.Kind == "" {
		return 0, invalid("kind is required")
	}
	if len(f.Errors) == 0 {
		f.Errors = json.RawMessage("[]")
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into feedback (kind, text, voice_text, app_version, android_version, device, operator, screen, errors, provider)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id`,
		f.Kind, f.Text, f.VoiceText, f.AppVersion, f.AndroidVersion, f.Device, f.Operator, f.Screen, f.Errors, f.Provider).Scan(&id)
	return id, err
}

// RecentFeedback returns up to limit rows, newest first.
func (s *Store) RecentFeedback(ctx context.Context, limit int) ([]Feedback, error) {
	rows, err := s.pool.Query(ctx, `
		select id, created_at, kind, text, voice_text, app_version, android_version, device, operator, screen, errors, provider
		from feedback order by id desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Feedback, error) {
		var f Feedback
		return f, r.Scan(&f.ID, &f.CreatedAt, &f.Kind, &f.Text, &f.VoiceText, &f.AppVersion, &f.AndroidVersion,
			&f.Device, &f.Operator, &f.Screen, &f.Errors, &f.Provider)
	})
}

// ListFeedback returns up to limit rows with id greater than after, oldest first.
func (s *Store) ListFeedback(ctx context.Context, after int64, limit int) ([]Feedback, error) {
	rows, err := s.pool.Query(ctx, `
		select id, created_at, kind, text, voice_text, app_version, android_version, device, operator, screen, errors, provider
		from feedback where id > $1 order by id limit $2`, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Feedback, error) {
		var f Feedback
		return f, r.Scan(&f.ID, &f.CreatedAt, &f.Kind, &f.Text, &f.VoiceText, &f.AppVersion, &f.AndroidVersion,
			&f.Device, &f.Operator, &f.Screen, &f.Errors, &f.Provider)
	})
}
