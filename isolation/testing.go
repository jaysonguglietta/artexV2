package isolation

import (
	"os"
	"testing"
)

// TrustedFixtureMode is only reachable in Go test binaries, never through a
// production environment variable. It retains existing synthetic local fixtures.
func TrustedFixtureMode() bool {
	return testing.Testing() && os.Getenv("ARTEX_ALLOW_HOST_EXECUTION") == "1"
}
func ExecutionEnabled() bool { return Configured() || TrustedFixtureMode() }
