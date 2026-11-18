//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package isolation

import "errors"

// The remote API client remains portable; a local Docker broker requires a
// supported Unix host with an exclusive, no-follow persistent state ledger.
func (b *Broker) openLedger(string) error {
	return errors.New("the isolated worker broker requires a supported Unix host")
}
