package api

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/importer"
	"qoimeks/server/internal/store"
)

func TestStockOverviewAndExport(t *testing.T) {
	c, skus, locs := setup(t, &Server{AccessToken: "phone"})
	for loc, qty := range map[string]float64{"A-01": 100, "B-01": 5} {
		c.do("POST", "/api/movements", store.NewMovement{
			ID: uuid.New(), Kind: "receipt", SKUID: skus["NUT-M8"], LocationID: locs[loc], Qty: qty, Operator: "Айгуль",
		}, 201, nil)
	}

	var items []store.StockItem
	c.do("GET", "/api/stock", nil, 200, &items)
	var nut *store.StockItem
	for i := range items {
		if items[i].Code == "NUT-M8" {
			nut = &items[i]
		}
	}
	if nut == nil || nut.Total != 105 || len(nut.Places) != 2 || nut.Places[0].LocationCode != "A-01" || nut.Places[1].Qty != 5 {
		t.Fatalf("NUT-M8: %+v", nut)
	}
	if len(items) != len(skus) {
		t.Fatalf("%d items for %d SKUs: SKUs without stock must be listed too", len(items), len(skus))
	}

	// The page is open; the data and the file need the access code.
	if resp, err := http.Get(c.url + "/"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("page: %v %v", resp, err)
	}
	resp, err := http.Get(c.url + "/api/stock/export")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("export without a code: %v %v", resp, err)
	}
	req, _ := http.NewRequest("GET", c.url+"/api/stock/export", nil)
	req.Header.Set("Authorization", "Bearer phone")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != xlsxType ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
	}
	parsed, err := importer.ParseReader(resp.Body, "")
	if err != nil {
		t.Fatalf("the export is not readable by the importer: %v", err)
	}
	if len(parsed.SKUs) != len(skus) {
		t.Fatalf("export has %d SKUs, want %d", len(parsed.SKUs), len(skus))
	}
}

func getBody(t *testing.T, url, bearer string) (string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.Header
}

func TestWebSetupQR(t *testing.T) {
	const code = "s3cret-code-xyz"
	c, _, _ := setup(t, &Server{AccessToken: code})

	page, _ := getBody(t, c.url+"/", "")
	for _, want := range []string{
		`id="setup"`, "Подключить приложение", "qoimeks://setup?url=", "&code=",
		"никому не показывайте", "function setupPayload", "function qrMatrix", "function renderSetup",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	// The QR is drawn in the browser from what the page already knows: nothing external.
	external := regexp.MustCompile(`(?i)<script[^>]*\ssrc=|(src|href)\s*=\s*["']?(https?:)?//|@import|url\(\s*["']?(https?:)?//|<link[^>]+stylesheet|fetch\(['"]https?:`)
	if m := external.FindString(page); m != "" {
		t.Errorf("page uses an external resource: %q", m)
	}
	// renderSetup runs at load and whenever the signed-in state changes.
	if n := strings.Count(page, "renderSetup();"); n < 4 {
		t.Errorf("renderSetup is called %d times, want load, sign-in success, sign-in failure and sign-out", n)
	}

	// The access code reaches no response of a caller who does not have it, and there is no
	// endpoint made for setup.
	for _, bearer := range []string{"", "wrong-code"} {
		for _, path := range []string{"/", "/admin/", "/healthz", "/api/stock", "/api/stock/export", "/api/imports", "/api/catalog", "/api/balances", "/api/movements"} {
			body, header := getBody(t, c.url+path, bearer)
			if strings.Contains(body, code) || strings.Contains(strings.Join(header.Values("Set-Cookie"), ";"), code) {
				t.Errorf("GET %s with bearer %q returns the access code", path, bearer)
			}
		}
	}
	// Even with the right code these paths are not found: there is no endpoint for the setup QR.
	for _, path := range []string{"/setup", "/api/setup", "/qr", "/api/qr"} {
		req, _ := http.NewRequest("GET", c.url+path, nil)
		req.Header.Set("Authorization", "Bearer "+code)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("GET %s: status %d, want 404", path, resp.StatusCode)
		}
	}
}

// The pure functions between the setup-qr markers run under node when it is installed.
func TestWebSetupQRLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the page is checked as text in TestWebSetupQR")
	}
	html, err := os.ReadFile("web.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	i, j := strings.Index(page, "// --- setup-qr begin"), strings.Index(page, "// --- setup-qr end")
	if i < 0 || j < i {
		t.Fatal("setup-qr markers not found in web.html")
	}
	// HELLO, byte mode, level M, version 1. Produced by the encoder in web.html; every size
	// from version 1 to 10 was decoded with OpenCV's QRCodeDetector (an independent decoder)
	// when the encoder was written, which is what makes this vector trustworthy.
	hello := []string{
		"111111101101001111111", "100000100110101000001", "101110100111101011101", "101110101001001011101",
		"101110101000101011101", "100000101011001000001", "111111101010101111111", "000000001111100000000",
		"100010111111011111001", "000111001011100101111", "101100101011001110010", "111001000100011010000",
		"001011100100111000110", "000000001110111001011", "111111101100110001010", "100000100001100100010",
		"101110101001001110101", "101110100001100001011", "101110100111001111000", "100000100100011000000",
		"111111101000111110101",
	}
	harness := page[i:j] + `
const assert = require('assert');
assert.strictEqual(setupPayload('https://h', ''), 'qoimeks://setup?url=https%3A%2F%2Fh');
assert.strictEqual(setupPayload('https://h', undefined), 'qoimeks://setup?url=https%3A%2F%2Fh');
assert.strictEqual(setupPayload('https://h', 'a b&c'), 'qoimeks://setup?url=https%3A%2F%2Fh&code=a%20b%26c');
assert.strictEqual(setupPayload('https://qoimeks.ai-dala.com', 's3cret code'),
  'qoimeks://setup?url=https%3A%2F%2Fqoimeks.ai-dala.com&code=s3cret%20code');
assert.strictEqual(setupPayload('http://h:8080', 'пароль'), 'qoimeks://setup?url=http%3A%2F%2Fh%3A8080&code=%D0%BF%D0%B0%D1%80%D0%BE%D0%BB%D1%8C');
const hello = ["` + strings.Join(hello, `","`) + `"];
assert.deepStrictEqual(qrMatrix('HELLO').map(r => r.map(d => d ? '1' : '0').join('')), hello);
// Sizes grow with the text: version 1 holds 14 bytes at level M, version 10 holds 213.
assert.strictEqual(qrMatrix('x'.repeat(14)).length, 21);
assert.strictEqual(qrMatrix('x'.repeat(15)).length, 25);
assert.strictEqual(qrMatrix('x'.repeat(213)).length, 57);
assert.throws(() => qrMatrix('x'.repeat(214)));
// The url-only payload fits a small QR; with a long code it is still within capacity.
const url = setupPayload('https://qoimeks.ai-dala.com', '');
assert.ok(qrMatrix(url).length <= 33);
assert.doesNotThrow(() => qrMatrix(setupPayload('https://qoimeks.ai-dala.com', 'x'.repeat(40))));
`
	file := filepath.Join(t.TempDir(), "setup-qr.js")
	if err := os.WriteFile(file, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, file).CombinedOutput(); err != nil {
		t.Fatalf("node: %v: %s", err, out)
	}
}

// The page carries the design tokens inline (light theme) and asks the network for nothing.
func TestWebPageUsesInlineDesignTokens(t *testing.T) {
	html, err := os.ReadFile("web.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, want := range []string{
		"--c-primary: #14304f;", "--c-on-primary: #ffffff;", "--c-bg: #f6f1e7;", "--font-mono:", "--size-touch-min: 48px;",
		"background: var(--c-primary)", "td.num, th.num { text-align: right;",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("web.html lacks %q", want)
		}
	}
	if strings.Contains(page, `data-theme="dark"`) {
		t.Error("the page is light only")
	}
	if m := regexp.MustCompile(`(?i)<(link|script|img|iframe)[^>]+(src|href)\s*=\s*["']?(https?:)?//`).FindString(page); m != "" {
		t.Errorf("web.html makes an external request: %s", m)
	}
	if strings.Contains(page, "@import") || strings.Contains(page, "url(http") {
		t.Error("web.html imports an external stylesheet or font")
	}
	// Colours come from tokens: after the token block no hex literal is left in the rules.
	rules := page[strings.Index(page, "* { box-sizing"):strings.Index(page, "</style>")]
	if hex := regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b`).FindString(rules); hex != "" {
		t.Errorf("colour literal %s outside the token block", hex)
	}
}
