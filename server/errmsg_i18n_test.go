package server

import (
	"github.com/Autumn-27/artex/config"
	"testing"
)

func TestBackendEnglishOnly(t *testing.T) {
	t.Cleanup(func() { config.CurrentLanguage = nil })
	for _, saved := range []string{"en", "ko", "zh", "es", "ja", ""} {
		config.CurrentLanguage = func() string { return saved }
		if config.ActiveLanguage() != "en" {
			t.Fatal("saved locale changed output language")
		}
		assertEnglishError(t, "authentication", trMsg(authErrUnauthorized))
	}
}
