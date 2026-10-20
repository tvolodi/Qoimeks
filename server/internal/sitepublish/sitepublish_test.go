// Package sitepublish only holds tests of the download site template and its publish
// script, so that `go test ./...` covers them.
package sitepublish

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const repo = "../../.."

func TestPublishScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	out, err := exec.Command(bash, filepath.Join(repo, "scripts", "test-publish.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("test-publish.sh: %v\n%s", err, out)
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, "deploy", "qoimeks-site", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTemplatePage(t *testing.T) {
	page := read(t, "index.html")
	for _, want := range []string{
		`lang="ru"`, `lang="kk"`,
		"Установка неизвестных приложений", "Белгісіз қосымшаларды орнату",
		"0.1.0", "Заново вводятся только адрес сервера, код доступа и имя",
		"Тек сервер мекенжайы, қолжетімділік коды және аты қайта енгізіледі",
		"<svg", `id="download"`, `id="fallback"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index.html lacks %q", want)
		}
	}

	// No external resource: the only absolute-looking reference allowed is none at all.
	external := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9]`)
	for _, name := range []string{"index.html", "site.css", "site.js"} {
		if m := external.FindString(read(t, name)); m != "" {
			t.Errorf("%s references an external address: %q", name, m)
		}
	}
	// Local data source is the manifest; the fallback is a link to it.
	if !strings.Contains(page, `<a href="./latest.json">`) {
		t.Error("fallback link to ./latest.json missing")
	}
}

func TestTemplateScript(t *testing.T) {
	js := read(t, "site.js")
	for _, want := range []string{
		`fetch("./latest.json"`, "m.versionName", "m.notes && m.notes.ru", "m.notes && m.notes.kk", `setAttribute("href", m.url)`,
		`getElementById("fallback").hidden = false`, `navigator.language`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("site.js lacks %q", want)
		}
	}
	// The fallback message is visible in the markup itself, so it also shows without JavaScript.
	if strings.Contains(read(t, "index.html"), `id="fallback" class="card warn" hidden`) {
		t.Error("fallback must be visible by default")
	}
}
