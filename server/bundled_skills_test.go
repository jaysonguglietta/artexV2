package server

import (
	"path/filepath"
	"testing"
	"unicode"

	"github.com/Autumn-27/norma/skill"
)

func TestBundledSkillsExposeEnglishMetadata(t *testing.T) {
	registry, err := skill.LoadDir(filepath.Join("..", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"api-recon", "playwright-cli", "scopesentry"} {
		item, ok := registry.Get(name)
		if !ok || item.Description == "" || item.Instructions == "" {
			t.Fatalf("skill %s has missing metadata or instructions", name)
		}
		for _, r := range item.Description {
			if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) {
				t.Fatalf("skill %s has untranslated metadata", name)
			}
		}
	}
}
