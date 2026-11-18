package storage

import (
	"errors"
	"runtime"
	"strings"
)

// ValidatePath rejects Windows alternate streams and device-name aliases, which
// are not visible as ordinary directory entries to quota accounting.
func ValidatePath(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return validateWindowsPath(path)
}

func validateWindowsPath(path string) error {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(normalized, "//?/") || strings.HasPrefix(normalized, "//./") {
		return errors.New("Windows device namespaces are not managed storage paths")
	}
	if len(normalized) >= 2 && normalized[1] == ':' && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) {
		normalized = normalized[2:]
	}
	if strings.Contains(normalized, ":") {
		return errors.New("Windows alternate data streams are not managed storage paths")
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if strings.TrimRight(component, " .") != component {
			return errors.New("Windows storage path components must not end in spaces or dots")
		}
		base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return errors.New("Windows device aliases are not managed storage paths")
		}
	}
	return nil
}
