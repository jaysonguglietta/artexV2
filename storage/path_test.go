package storage

import "testing"

func TestWindowsPathsRejectUncountedStreamsAndAliases(t *testing.T) {
	for _, path := range []string{`C:\data\tasks\report.txt:secret`, `tasks/report:stream:$DATA`, `C:\data\CON.txt`, `tasks/aux`, `tasks/COM1.log`, `tasks/lpt9`, `tasks/part.`, `tasks/part `, `\\?\C:\data\task`, `\\.\NUL`} {
		if err := validateWindowsPath(path); err == nil {
			t.Errorf("accepted unsafe Windows path %q", path)
		}
	}
	for _, path := range []string{`C:\data\tasks\report.txt`, `D:/data/tasks/file`, `tasks/normal.txt`, `tasks/com10.txt`} {
		if err := validateWindowsPath(path); err != nil {
			t.Errorf("refused ordinary Windows path %q: %v", path, err)
		}
	}
}
