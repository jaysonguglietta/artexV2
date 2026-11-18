package guard

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Setenv("ARTEX_ALLOW_HOST_EXECUTION", "1"); os.Exit(m.Run()) }
