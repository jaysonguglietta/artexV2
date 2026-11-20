package db

import "testing"

func TestTaskPauseCommittedAtCreation(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, paused := range []bool{true, false} {
		task, err := d.CreateTaskWithOptions("guided website", "baseline", TaskCreateOptions{StartPaused: paused})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
		loaded, err := d.GetTask(task.ID)
		if err != nil || loaded == nil || loaded.Paused != paused || task.Paused != paused {
			t.Fatalf("pause not committed: task=%+v loaded=%+v err=%v", task, loaded, err)
		}
	}
}
