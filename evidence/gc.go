package evidence

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Autumn-27/artex/storage"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

var gcCursors = struct {
	sync.Mutex
	after map[string]string
}{after: map[string]string{}}

// collectBatch bounds SQL metadata changes and blob inspections independently.
// The evidence advisory lock pins every committed binding through this pass.
func (s *Store) collectBatch(ctx context.Context, now time.Time, grace time.Duration) error {
	bucket, err := storage.Ensure(s.Dir, storage.Evidence)
	if err != nil {
		return err
	}
	return s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		return bucket.WithUnheld(func() error {
			if _, err := tx.ExecContext(ctx, `UPDATE traffic_evidence_snapshots SET unreferenced_at=$1 WHERE id IN(SELECT s.id FROM traffic_evidence_snapshots s WHERE unreferenced_at IS NULL AND NOT EXISTS(SELECT 1 FROM finding_traffic_bindings b WHERE b.snapshot_id=s.id) LIMIT 1000)`, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_evidence_snapshots WHERE id IN(SELECT s.id FROM traffic_evidence_snapshots s WHERE unreferenced_at<=$1 AND NOT EXISTS(SELECT 1 FROM finding_traffic_bindings b WHERE b.snapshot_id=s.id) LIMIT 1000)`, now.Add(-grace)); err != nil {
				return err
			}
			root, err := os.OpenRoot(s.Dir)
			if err != nil {
				return err
			}
			defer root.Close()
			gcCursors.Lock()
			after := gcCursors.after[s.Dir]
			gcCursors.Unlock()
			last := ""
			inspected := 0
			reachedLimit := false
			err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if err = ctx.Err(); err != nil {
					return err
				}
				if name == "." {
					return nil
				}
				if entry.IsDir() {
					if name != "blobs" && name != ".staging" && !strings.HasPrefix(name, "blobs/") {
						return fs.SkipDir
					}
					return nil
				}
				if name <= after {
					return nil
				}
				if inspected >= 1000 {
					reachedLimit = true
					return fs.SkipAll
				}
				inspected++
				last = name
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				if grace > 0 && now.Sub(info.ModTime()) < grace {
					return nil
				}
				if strings.HasPrefix(name, "blobs/") {
					hash := strings.TrimSuffix(entry.Name(), ".bin")
					var referenced bool
					if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM traffic_evidence_snapshots WHERE req_hash=$1 OR resp_hash=$1)`, hash).Scan(&referenced); err != nil {
						return err
					}
					if referenced {
						return nil
					}
				}
				if err = root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				return nil
			})
			if err != nil {
				return err
			}
			gcCursors.Lock()
			if reachedLimit {
				gcCursors.after[s.Dir] = last
			} else {
				delete(gcCursors.after, s.Dir)
			}
			gcCursors.Unlock()
			return nil
		})
	})
}

// PurgeUnreferenced removes one bounded batch without the orphan grace period.
// It never removes a body/snapshot referenced by an existing finding binding.
func (s *Store) PurgeUnreferenced(ctx context.Context, now time.Time) error {
	return s.collectBatch(ctx, now, 0)
}
