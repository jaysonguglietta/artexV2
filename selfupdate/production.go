package selfupdate

import (
	"errors"
	"testing"
)

var errExternalVerification = errors.New("In-process updates are disabled. Verify signed release artifacts with scripts/signed-release.py before installing or redeploying")

// Legacy staging code remains covered by real Go tests. No environment variable,
// HTTP request or staged marker enables unsigned replacement in production.
func runtimeUpdatesAllowed() bool { return testing.Testing() }
