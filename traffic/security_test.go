package traffic

import "testing"

func TestProxyRejectsPublicListenAddress(t *testing.T) {
	for _, addr := range []string{":8788", "0.0.0.0:8788", "[::]:8788", "192.0.2.1:8788"} {
		if proxy, err := Open(t.TempDir(), addr); err == nil {
			proxy.Close()
			t.Fatalf("accepted public proxy address %q", addr)
		}
	}
}
