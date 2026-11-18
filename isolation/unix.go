package isolation

import (
	"context"
	"net"
	"time"
)

func unixDial(ctx context.Context, _, _ string) (net.Conn, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := (&net.Dialer{}).DialContext(ctx, "unix", "/tmp/artex-tool-worker.sock")
		if err == nil || ctx.Err() != nil || time.Now().After(deadline) {
			return conn, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
