package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"qoimeks/server/internal/store"
)

// failureWindow is how long a repeat of the same failure is not recorded again.
const failureWindow = 60 * time.Second

type failKey struct{ schema, kind, provider string }

// isServerFailure reports whether a store error ends as a 5xx, not as a validation answer.
func isServerFailure(err error) bool {
	return !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrInvalid)
}

// appHeader returns a header the app sends about itself (X-App-Version, X-Device, ...):
// trimmed, percent-decoded and cut to limit runes.
func appHeader(r *http.Request, name string, limit int) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if u, err := url.PathUnescape(v); err == nil { // the app percent-encodes, headers cannot carry Cyrillic
		v = u
	}
	if utf8.RuneCountInString(v) > limit {
		v = string([]rune(v)[:limit])
	}
	return v
}

// recordFailure appends a feedback row for a failure the user did not report: kind is
// speech_error, llm_error or post_error. It goes to the schema of the request's tenant.
// At most one row per (schema, kind, provider) is appended per failureWindow; rows are
// never updated. Recording never fails the request.
func (s *Server) recordFailure(r *http.Request, kind, provider string, cause error) {
	schema, _ := r.Context().Value(schemaKey{}).(string)
	key := failKey{schema, kind, provider}
	now := s.clock()
	s.failMu.Lock()
	if last, ok := s.failSeen[key]; ok && now.Sub(last) < failureWindow {
		s.failMu.Unlock()
		return
	}
	if s.failSeen == nil {
		s.failSeen = map[failKey]time.Time{}
	}
	s.failSeen[key] = now
	s.failMu.Unlock()

	d := s.newRedactor(r)
	header := func(name string) string { return d.str(appHeader(r, name, maxFeedbackField)) }
	msg := d.str(cause.Error())
	class := fmt.Sprintf("%T", cause)
	details, _ := json.Marshal([]map[string]string{{"class": class, "message": msg}})
	prov := d.str(provider)
	f := store.Feedback{
		Kind:       kind,
		Text:       msg,
		AppVersion: header("X-App-Version"),
		Device:     header("X-Device"),
		Operator:   header("X-Operator"),
		Errors:     details,
		Provider:   &prov,
	}
	// The request may already be cancelled (the client gave up); the row is still wanted.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if _, err := s.store(r).AppendFeedback(ctx, f); err != nil {
		slog.Error("recording failure", "kind", kind, "err", err)
	}
}
