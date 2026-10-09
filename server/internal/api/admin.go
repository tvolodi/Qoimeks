package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"qoimeks/server/internal/importer"
	"qoimeks/server/internal/store"
)

//go:embed admin.html
var adminPage []byte

const maxWorkbookBytes = 12 << 20

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(adminPage)
	})
	mux.HandleFunc("POST /admin/api/import/preview", s.admin(func(w http.ResponseWriter, r *http.Request) { s.importWorkbook(w, r, false) }))
	mux.HandleFunc("POST /admin/api/import/apply", s.admin(func(w http.ResponseWriter, r *http.Request) { s.importWorkbook(w, r, true) }))
	mux.HandleFunc("GET /admin/api/imports", s.admin(s.importHistory))
}

// admin guards a route with AdminToken, which is separate from the access code
// that sits on every phone. Without a configured token the route is closed.
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	want := []byte("Bearer " + s.AdminToken)
	return func(w http.ResponseWriter, r *http.Request) {
		if s.AdminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "administration is not configured on the server")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeError(w, http.StatusUnauthorized, "admin code is missing or wrong")
			return
		}
		next(w, r)
	}
}

type importResponse struct {
	Applied   bool               `json:"applied"`
	SHA256    string             `json:"sha256"`
	Sheet     string             `json:"sheet"`
	Sheets    []string           `json:"sheets"`
	SKUs      int                `json:"skus"`
	Locations int                `json:"locations"`
	Remarks   []string           `json:"remarks"`
	Changes   store.ImportResult `json:"changes"`
}

// importWorkbook reads the uploaded workbook in memory and reports what loading it
// changes. A preview rolls everything back. Applying needs the checksum of the
// previewed file, so what gets loaded is exactly what the administrator looked at.
func (s *Server) importWorkbook(w http.ResponseWriter, r *http.Request, apply bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkbookBytes)
	// Larger than the body limit, so the upload never spills into a temporary file.
	if err := r.ParseMultipartForm(maxWorkbookBytes + 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "the file is missing or larger than 12 MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "the file is missing")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the file")
		return
	}
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	who := strings.TrimSpace(r.FormValue("who"))
	if apply && who == "" {
		writeError(w, http.StatusBadRequest, "your name is required")
		return
	}
	if apply && r.FormValue("sha256") != checksum {
		writeError(w, http.StatusConflict, "this is not the file that was checked; check it again")
		return
	}

	parsed, err := importer.ParseReader(bytes.NewReader(data), r.FormValue("sheet"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "cannot read the workbook: "+err.Error())
		return
	}
	withBalances := r.FormValue("balances") == "true"
	opts := store.ImportOptions{PostBalances: withBalances, DryRun: !apply, Operator: "импорт " + parsed.Sheet}
	if apply {
		opts.Record = &store.ImportRecord{
			Who: who, Filename: header.Filename, Sheet: parsed.Sheet, SHA256: checksum,
			WithBalances: withBalances, Remarks: len(parsed.Warnings),
		}
	}
	changes, err := s.Store.Import(r.Context(), parsed.Locations, parsed.SKUs, opts)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if apply {
		slog.Info("workbook imported", "who", who, "file", header.Filename, "sheet", parsed.Sheet, "changes", changes)
	}
	remarks := parsed.Warnings
	if remarks == nil {
		remarks = []string{}
	}
	writeJSON(w, http.StatusOK, importResponse{
		Applied: apply, SHA256: checksum, Sheet: parsed.Sheet, Sheets: parsed.Sheets,
		SKUs: len(parsed.SKUs), Locations: len(parsed.Locations), Remarks: remarks, Changes: changes,
	})
}

func (s *Server) importHistory(w http.ResponseWriter, r *http.Request) {
	records, err := s.Store.Imports(r.Context(), 50)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if records == nil {
		records = []store.ImportRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}
