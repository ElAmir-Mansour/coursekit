package scan_test

import (
	"os/exec"
	"testing"
)

// execCommand runs a helper binary for fixture setup and returns its combined
// output, which is only surfaced when something goes wrong.
func execCommand(t *testing.T, name string, args ...string) ([]byte, error) {
	t.Helper()
	return exec.Command(name, args...).CombinedOutput()
}
