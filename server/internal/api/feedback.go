package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"qoimeks/server/internal/store"
)

const (
	maxFeedbackText   = 4000
	maxFeedbackField  = 200
	maxFeedbackErrors = 50
	maxFeedbackBody   = 1 << 20
	minSecretLen      = 8 // provider keys shorter than this are not redacted
	redacted          = "[redacted]"
)

var (
	bearerRe = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	skKeyRe  = regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`)
)

type feedbackRequest struct {
	Text           string            `json:"text"`
	VoiceText      *string           `json:"voice_text"`
	AppVersion     string            `json:"app_version"`
	AndroidVersion string            `json:"android_version"`
	Device         string            `json:"device"`
	Operator       string            `json:"operator"`
	Screen         string            `json:"screen"`
	Errors         []json.RawMessage `json:"errors"`
	Provider       *string           `json:"provider"`
}

// redactor replaces secrets in text before it is stored.
type redactor struct{ exact []string }

// newRedactor covers the access token, the code the request came with (the default
// token or the requesting tenant's code) and the provider keys of at least
// minSecretLen characters.
func (s *Server) newRedactor(r *http.Request) redactor {
	var exact []string
	if s.AccessToken != "" {
		exact = append(exact, s.AccessToken)
	}
	if code, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && code != "" {
		exact = append(exact, code)
	}
	for _, k := range s.ProviderKeys {
		if len(k) >= minSecretLen {
			exact = append(exact, k)
		}
	}
	// Longest first, so a secret that contains a shorter one is removed whole.
	sort.Slice(exact, func(i, j int) bool { return len(exact[i]) > len(exact[j]) })
	return redactor{exact: exact}
}

func (d redactor) str(v string) string {
	for _, e := range d.exact {
		v = strings.ReplaceAll(v, e, redacted)
	}
	v = bearerRe.ReplaceAllString(v, redacted)
	return skKeyRe.ReplaceAllString(v, redacted)
}

// value redacts every string (and object key) inside decoded JSON.
func (d redactor) value(v any) any {
	switch x := v.(type) {
	case string:
		return d.str(x)
	case []any:
		for i := range x {
			x[i] = d.value(x[i])
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[d.str(k)] = d.value(e)
		}
		return out
	}
	return v
}

func (s *Server) postFeedback(w http.ResponseWriter, r *http.Request) {
	var in feedbackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFeedbackBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(in.Text) == "" || utf8.RuneCountInString(in.Text) > maxFeedbackText {
		writeError(w, http.StatusBadRequest, "text must be 1 to 4000 characters")
		return
	}
	if in.VoiceText != nil && utf8.RuneCountInString(*in.VoiceText) > maxFeedbackText {
		writeError(w, http.StatusBadRequest, "voice_text must be at most 4000 characters")
		return
	}
	if len(in.Errors) > maxFeedbackErrors {
		writeError(w, http.StatusBadRequest, "errors must have at most 50 entries")
		return
	}
	meta := []string{in.AppVersion, in.AndroidVersion, in.Device, in.Operator, in.Screen}
	if in.Provider != nil {
		meta = append(meta, *in.Provider)
	}
	for _, m := range meta {
		if utf8.RuneCountInString(m) > maxFeedbackField {
			writeError(w, http.StatusBadRequest, "app_version, android_version, device, operator, screen and provider must be at most 200 characters")
			return
		}
	}

	d := s.newRedactor(r)
	errs := make([]any, len(in.Errors))
	for i, raw := range in.Errors {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			writeError(w, http.StatusBadRequest, "errors must be valid JSON")
			return
		}
		errs[i] = d.value(v)
	}
	errJSON, _ := json.Marshal(errs)
	f := store.Feedback{
		Kind:           "user",
		Text:           d.str(in.Text),
		AppVersion:     d.str(in.AppVersion),
		AndroidVersion: d.str(in.AndroidVersion),
		Device:         d.str(in.Device),
		Operator:       d.str(in.Operator),
		Screen:         d.str(in.Screen),
		Errors:         errJSON,
	}
	if in.VoiceText != nil {
		v := d.str(*in.VoiceText)
		f.VoiceText = &v
	}
	if in.Provider != nil {
		v := d.str(*in.Provider)
		f.Provider = &v
	}
	id, err := s.store(r).AppendFeedback(r.Context(), f)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}
