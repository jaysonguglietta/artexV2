package server

import (
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestReporterUpgradeRecognizesLegacyStoredMessage(t *testing.T) {
	s, _, _ := trafficEvidenceServer(t)
	pg := s.m.pg
	const key = "reporter_trigger_evidence_version_v1"
	prior, existed, err := pg.GetSetting(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = pg.SetSetting(key, prior)
		} else {
			_, _ = pg.Exec("DELETE FROM settings WHERE key=$1", key)
		}
	})
	if err := pg.SetSetting(key, "false"); err != nil {
		t.Fatal(err)
	}
	// Actual historical storage format, independent of the production comparison
	// constant. This is compatibility data, not a current language directive.
	const legacy = `上面刚有一个漏洞被 report_finding 登记。请从触发上下文里取出 finding_id（工具返回 "finding recorded: <id>" 里的数字）与任务 id，按你的职责撰写该漏洞的详细报告，最后调用 update_finding_report(finding_id, report) 保存。`
	expected := map[int64]string{}
	for _, message := range []string{legacy, "User-reviewed custom reporter instructions"} {
		trigger, err := pg.CreateTrigger(&db.AgentTrigger{AgentKey: "reporter", OnToolCall: true, ToolNames: []string{"report_finding"}, ToolCallMessage: message})
		if err != nil {
			t.Fatal(err)
		}
		id := trigger.ID
		t.Cleanup(func() { _ = pg.DeleteTrigger(id) })
		want := message
		if message == legacy {
			want = reporterToolCallMessage
		}
		expected[id] = want
	}
	s.upgradeReporterTriggerMessage()
	triggers, err := pg.ListTriggersFor("reporter")
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range triggers {
		if want, ok := expected[trigger.ID]; ok {
			if trigger.ToolCallMessage != want {
				t.Errorf("trigger %d did not preserve the migration contract", trigger.ID)
			}
			delete(expected, trigger.ID)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing test triggers: %v", expected)
	}
}
