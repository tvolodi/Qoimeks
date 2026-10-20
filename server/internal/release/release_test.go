// Package release only holds the test that runs the release script's shell test.
package release

import (
	"os/exec"
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
