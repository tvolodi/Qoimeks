package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"regexp"
	"time"

	"qoimeks/server/internal/importer"
)

//go:embed web.html
var webPage []byte

// The settings section is fenced in web.html by settings:begin / settings:end markers
// (HTML comments in markup, block comments in the script). Without SETTINGS_UI the
// server cuts the fenced parts out, so the link, the page and its code are never sent.
var (
	settingsHTMLFence = regexp.MustCompile(`(?s)<!--settings:begin-->.*?<!--settings:end-->\r?\n?`)
	settingsJSFence   = regexp.MustCompile(`(?s)/\*settings:begin\*/.*?/\*settings:end\*/`)

	webPageWithoutSettings = settingsJSFence.ReplaceAll(settingsHTMLFence.ReplaceAll(webPage, nil), nil)
)

const xlsxType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// webRoutes serves the stock overview page. The page itself is public; its data
// comes from /api/ and needs the access code like the phones do.
func (s *Server) webRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if s.SettingsUI {
			w.Write(webPage)
			return
		}
		w.Write(webPageWithoutSettings)
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
