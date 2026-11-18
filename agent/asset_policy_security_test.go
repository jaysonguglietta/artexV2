package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Schema fault injection is opt-in and runs only through the disposable DB harness.
func TestAssetInsertionFailsClosedWhenPolicyCannotBeRead(t *testing.T) {
	if os.Getenv("ARTEX_ISOLATED_TEST_DATABASE") != "1" {
		t.Skip("requires disposable database harness")
	}
	d := testDB(t)
	defer d.Close()
	task, err := d.CreateTask("Policy failure regression", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	ts := NewToolSet(d.Exploration(task.ExplorationID), "worker")
	ts.SetTaskID(task.ID)
	ts.SetAssetStore(d.Assets(), d.Companies())
	for _, table := range []string{"asset_intercept_rules", "task_intercept_rules"} {
		t.Run(table, func(t *testing.T) {
			// Names come exclusively from the constant table list above. The rename
			// simulates unavailable policy while leaving the asset table writable.
			shadow := table + "_policy_fault"
			if _, err := d.Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", table, shadow)); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := d.Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", shadow, table)); err != nil {
					t.Errorf("restore policy table: %v", err)
				}
			}()
			result, err := ts.insertAssets().Call(t.Context(), json.RawMessage(`{"assets":[{"type":"root_domain","domain":"policy-failure.example"}]}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(result.Flatten(), "insertion refused") {
				t.Fatalf("policy failure allowed insertion: %s", result.Flatten())
			}
			var count int
			if err := d.QueryRow("SELECT count(*) FROM assets WHERE domain=$1", "policy-failure.example").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("inserted %d assets without policy", count)
			}
		})
	}
}
