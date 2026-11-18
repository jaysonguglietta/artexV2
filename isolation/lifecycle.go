package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// RetireTask drains an isolated runtime before archive/deletion. A preserved
// snapshot is applied before removing the container; failed writes resume it.
func RetireTask(ctx context.Context, taskID, dataDir string, discard bool) (err error) {
	if !Configured() {
		return nil
	}
	id, parseErr := strconv.ParseInt(taskID, 10, 64)
	if parseErr != nil || id <= 0 {
		return errors.New("invalid task lifecycle identity")
	}
	session := "task-" + strconv.FormatInt(id, 10)
	paused := false
	defer func() {
		if err != nil && paused {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = Call(cleanup, Request{Session: session, Tool: "__resume"})
		}
	}()
	if !discard {
		root := filepath.Join(dataDir, "tasks", taskID)
		if err = os.MkdirAll(root, 0700); err != nil {
			return err
		}
		var sent map[string][]byte
		sent, err = ReadFiles(root, 4<<20)
		if err != nil {
			return err
		}
		var snapshot Response
		paused = true // Also attempt recovery when a snapshot response is lost.
		snapshot, err = Call(ctx, Request{Session: session, Tool: "__snapshot"})
		if err != nil {
			return err
		}
		paused = snapshot.SnapshotComplete
		if snapshot.SnapshotComplete {
			if err = ApplySnapshot(root, sent, snapshot.Files); err != nil {
				return err
			}
		}
	}
	_, err = Call(ctx, Request{Session: session, Tool: "__retire"})
	return err
}
