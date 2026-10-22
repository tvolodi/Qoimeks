package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"time"

	"qoimeks/server/internal/importer"
)

//go:embed web.html
var webPage []byte

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// webRoutes serves the stock overview page. The page itself is public; its data
// comes from /api/ and needs the access code like the phones do.
func (s *Server) webRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(webPage)
	})
	mux.HandleFunc("GET /api/stock", s.stock)
	mux.HandleFunc("GET /api/stock/export", s.stockWorkbook)
}

func (s *Server) stock(w http.ResponseWriter, r *http.Request) {
	items, err := s.store(r).Stock(r.Context())
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// stockWorkbook exports the current stock in the layout the importer reads.
func (s *Server) stockWorkbook(w http.ResponseWriter, r *http.Request) {
	items, err := s.store(r).Stock(r.Context())
	if err != nil {
		storeError(w, r, err)
		return
	}
	day := time.Now().Format("2006-01-02")
	var buf bytes.Buffer
	if err := importer.Write(&buf, "Остатки "+day, items); err != nil {
		storeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", xlsxType)
	w.Header().Set("Content-Disposition", `attachment; filename="stock-`+day+`.xlsx"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}
