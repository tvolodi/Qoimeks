// Package api is the HTTP interface used by the mobile app.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/stt"
)

const maxAudioBytes = 10 << 20

type Server struct {
	Store *store.Store
	// Parser and Transcriber may be nil when their service is not configured;
	// the app then works from the keyboard only.
	Parser      parse.Parser
	Transcriber stt.Transcriber
	// AccessToken, when set, must accompany every /api/ request. Leave it empty
	// only where the network itself keeps strangers out. It selects the default
	// schema; a tenant's own access code selects that tenant's schema.
	AccessToken string
	// ProvisioningToken guards /admin/v1 (tenant provisioning). Empty turns that API off.
	ProvisioningToken string
	// PublicURL is the server address returned to the site as a tenant's server_url.
	PublicURL string
	// ProviderKeys are the configured speech and language-model keys; feedback is
	// scrubbed of them before it is stored.
	ProviderKeys []string

	now     func() time.Time // tests replace the clock of the admin rate limiter
	mu      sync.Mutex
	tenants map[string]*store.Store // schema -> store, opened on first use
}

type storeKey struct{}

// store is the ledger a request works on: its tenant's, or the default one.
func (s *Server) store(r *http.Request) *store.Store {
	if st, ok := r.Context().Value(storeKey{}).(*store.Store); ok {
		return st
	}
	return s.Store
}

// tenantStore returns the cached store of a tenant schema, migrating it on first open.
func (s *Server) tenantStore(ctx context.Context, schema string) (*store.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.tenants[schema]; ok {
		return st, nil
	}
	st, err := s.Store.TenantStore(ctx, schema)
	if err != nil {
		return nil, err
	}
	if s.tenants == nil {
		s.tenants = map[string]*store.Store{}
	}
	s.tenants[schema] = st
	return st, nil
}

// Close releases the pools of the tenant stores.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, st := range s.tenants {
		st.Close()
		delete(s.tenants, k)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/balances", s.balances)
	mux.HandleFunc("GET /api/movements", s.movements)
	mux.HandleFunc("POST /api/movements", s.postMovement)
	mux.HandleFunc("POST /api/movements/{id}/reverse", s.reverseMovement)
	mux.HandleFunc("POST /api/parse", s.parseText)
	mux.HandleFunc("POST /api/voice", s.parseVoice)
	mux.HandleFunc("POST /api/feedback", s.postFeedback)
	s.webRoutes(mux)
	s.importRoutes(mux)
	s.adminRoutes(mux)
	return cors(s.requireToken(mux))
}

// requireToken guards /api/. A bearer that is a tenant's access code selects that
// tenant's schema (403 while suspended). Otherwise the shared AccessToken applies and
// selects the default schema; with no AccessToken set the default schema stays open.
func (s *Server) requireToken(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.AccessToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		isDefault := s.AccessToken != "" && subtle.ConstantTimeCompare([]byte(got), want) == 1
		if code, ok := strings.CutPrefix(got, "Bearer "); ok && code != "" && !isDefault {
			t, err := s.Store.TenantByCode(r.Context(), code)
			switch {
			case err == nil:
				switch t.EffectiveStatus(s.clock()) {
				case store.TenantExpired:
					writeError(w, http.StatusForbidden, "tenant expired")
					return
				case store.TenantActive:
				default:
					writeError(w, http.StatusForbidden, "tenant suspended")
					return
				}
				ts, err := s.tenantStore(r.Context(), t.SchemaName)
				if err != nil {
					storeError(w, r, err)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), storeKey{}, ts))
				next.ServeHTTP(w, r)
				return
			case !errors.Is(err, store.ErrNotFound):
				storeError(w, r, err)
				return
			}
		}
		if s.AccessToken != "" && !isDefault {
			writeError(w, http.StatusUnauthorized, "access code is missing or wrong")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cors lets the web build of the app, served from another origin, call the API.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("request failed", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	c, err := s.store(r).Catalog(r.Context())
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) balances(w http.ResponseWriter, r *http.Request) {
	skuID, err := strconv.ParseInt(r.URL.Query().Get("sku_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "sku_id is required")
		return
	}
	b, err := s.store(r).Balances(r.Context(), skuID)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) movements(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	m, err := s.store(r).Movements(r.Context(), limit)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) postMovement(w http.ResponseWriter, r *http.Request) {
	var in store.NewMovement
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	in.Operator = strings.TrimSpace(in.Operator)
	if in.Source == "" {
		in.Source = "keyboard"
	}
	m, created, err := s.store(r).Post(r.Context(), in)
	if err != nil {
		storeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, m)
}

func (s *Server) reverseMovement(w http.ResponseWriter, r *http.Request) {
	originalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid movement id")
		return
	}
	var in struct {
		ID       uuid.UUID `json:"id"`
		Operator string    `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	m, created, err := s.store(r).Reverse(r.Context(), originalID, in.ID, strings.TrimSpace(in.Operator))
	if err != nil {
		storeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, m)
}

// DraftResponse is a parsed phrase resolved against the catalog. Nothing here is
// saved: the operator reviews it in the form and posts it as a movement.
type DraftResponse struct {
	Transcript string          `json:"transcript"`
	Kind       string          `json:"kind,omitempty"`
	SKUs       []store.SKU     `json:"skus"` // best match first; more than one means "ask the operator"
	Location   *store.Location `json:"location,omitempty"`
	ToLocation *store.Location `json:"to_location,omitempty"`
	Qty        *float64        `json:"qty,omitempty"`
}

func (s *Server) parseText(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	s.draft(w, r, strings.TrimSpace(in.Text))
}

func (s *Server) parseVoice(w http.ResponseWriter, r *http.Request) {
	if s.Transcriber == nil {
		writeError(w, http.StatusServiceUnavailable, "speech recognition is not configured on the server")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAudioBytes)
	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "audio file is required (max 10 MB)")
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read audio")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	text, err := s.Transcriber.Transcribe(ctx, audio, header.Filename, r.FormValue("lang"))
	if err != nil {
		slog.Error("transcription failed", "err", err)
		writeError(w, http.StatusBadGateway, "speech recognition failed")
		return
	}
	if s.Parser == nil && text != "" {
		// Speech works but nothing can read the phrase yet: show the operator what
		// was heard and let them fill the form by hand.
		writeJSON(w, http.StatusOK, DraftResponse{Transcript: text, SKUs: []store.SKU{}})
		return
	}
	s.draft(w, r, text)
}

func (s *Server) draft(w http.ResponseWriter, r *http.Request, text string) {
	if text == "" {
		writeError(w, http.StatusUnprocessableEntity, "nothing was said")
		return
	}
	if s.Parser == nil {
		writeError(w, http.StatusServiceUnavailable, "phrase parsing is not configured on the server")
		return
	}
	catalog, err := s.store(r).Catalog(r.Context())
	if err != nil {
		storeError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := s.Parser.Parse(ctx, catalog, text)
	if err != nil {
		slog.Error("parsing failed", "err", err)
		writeError(w, http.StatusBadGateway, "could not understand the phrase")
		return
	}
	writeJSON(w, http.StatusOK, resolve(catalog, text, d))
}

// resolve keeps only what the catalog confirms; the model's output is never trusted as-is.
func resolve(c store.Catalog, text string, d parse.Draft) DraftResponse {
	out := DraftResponse{Transcript: text, SKUs: []store.SKU{}}
	switch d.Kind {
	case store.KindReceipt, store.KindIssue, store.KindTransfer, store.KindAdjustment:
		out.Kind = d.Kind
	}
	skus := make(map[string]store.SKU, len(c.SKUs))
	for _, v := range c.SKUs {
		skus[v.Code] = v
	}
	seen := map[string]bool{}
	for _, code := range d.SKUCodes {
		if v, ok := skus[code]; ok && !seen[code] {
			seen[code] = true
			out.SKUs = append(out.SKUs, v)
		}
	}
	// A location counts only if the model can point at the words it came from.
	spoken := func(words string) bool {
		words = strings.ToLower(strings.TrimSpace(words))
		return words != "" && strings.Contains(strings.ToLower(text), words)
	}
	for i, v := range c.Locations {
		if v.Code == d.LocationCode && spoken(d.LocationWords) {
			out.Location = &c.Locations[i]
		}
		if v.Code == d.ToLocationCode && spoken(d.ToLocationWords) && out.Kind == store.KindTransfer {
			out.ToLocation = &c.Locations[i]
		}
	}
	if d.Qty != nil && *d.Qty >= 0 {
		out.Qty = d.Qty
	}
	return out
}
