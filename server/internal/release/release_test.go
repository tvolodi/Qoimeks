// Package release only holds the test that runs the release script's shell test.
package release

import (
	"os/exec"
	"strings"
	"testing"
)

func TestReleaseScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	out, err := exec.Command(bash, "../../../scripts/test-release.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/test-release.sh failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// The usage text names --server-url (refusals are covered by scripts/test-release.sh).
func TestReleaseServerURLOption(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	out, _ := exec.Command(bash, "../../../scripts/release-android.sh", "--help").CombinedOutput()
	if !strings.Contains(string(out), "--server-url") {
		t.Errorf("usage does not mention --server-url:\n%s", out)
	}
}
