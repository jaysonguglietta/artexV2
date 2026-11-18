package db

import "time"

// Revocations are durable across server restarts; only token digests are stored.
func (d *DB) SessionRevoked(digest string) (bool, error) {
	var revoked bool
	err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM auth_revocations WHERE digest=$1 AND expires_at>now())`, digest).Scan(&revoked)
	return revoked, err
}
func (d *DB) RevokeSession(digest string, expires time.Time) error {
	_, err := d.Exec(`INSERT INTO auth_revocations(digest,expires_at) VALUES($1,$2) ON CONFLICT(digest) DO NOTHING`, digest, expires)
	if err != nil {
		return err
	}
	_, err = d.Exec(`DELETE FROM auth_revocations WHERE expires_at<=now()`)
	return err
}
