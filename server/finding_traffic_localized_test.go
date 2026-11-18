package server

import "testing"

// Pin English finding-traffic HTTP errors whose handlers require stored findings. Shared tool results/preview validation remain semantically tested separately, with raw evidence preserved.
func TestFindingTrafficErrorsLocalized(t *testing.T) {
	for _, c := range []struct {
		label, msg string
	}{
		{"inherited_readonly", errFindingTrafficInheritedReadonly},
		{"select_required", errFindingTrafficSelectRequired},
		{"version_required", errFindingTrafficVersionRequired},
		{"binding_ids_required", errFindingTrafficBindingIDsRequired},
	} {
		assertEnglishError(t, c.label, c.msg)
	}
}
