package agent

import (
	"encoding/json"
	"fmt"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

const findingIDGuidance = "\n\nFinding ID contract: finding_id is the independent finding record ID; finding_node_id is the exploration node ID. The id returned by list_findings / list_task_findings / node_detail / get_task_node_detail remains the exploration node ID; read finding_id from that same response for the independent record. get_finding_traffic / bind_finding_traffic require the independent finding_id. The legacy update_finding_report parameter named finding_id still takes finding_node_id. Do not use report_finding's first-line number for evidence tools or guess other IDs after an ID error."

// The server supplies the persisted setting. A missing setting/host is off.
// Consulted at assembly and again on writes so an already-running session
// cannot keep binding after the user switches the feature off.
var FindingTrafficBindingEnabled func() bool

func findingTrafficBindingEnabled() bool {
	return FindingTrafficBindingEnabled != nil && FindingTrafficBindingEnabled()
}

// Applied after ToolResolve: user descriptions and prompts remain intact, while
// all actual reporters (including Planner and custom chat agents) see the same
// API contract. Disabled/unbound tools are never reintroduced here.
func findingWorkflowTools(agentKey string, tools []actool.CoreTool) ([]actool.CoreTool, string) {
	if !findingTrafficBindingEnabled() {
		out := make([]actool.CoreTool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name() == "bind_finding_traffic" {
				continue
			}
			if agentKey == "reporter" && (tool.Name() == "traffic_search" || tool.Name() == "traffic_get" || tool.Name() == "traffic_blob") {
				continue
			}
			switch tool.Name() {
			case "report_finding", "add_hint", "add_task_hint":
				// Work on a copy: toggling back on must restore the original schema.
				raw, _ := json.Marshal(tool.InputSchema())
				var schema map[string]any
				if json.Unmarshal(raw, &schema) == nil {
					stripTrafficParameters(schema)
					tool = DecorateTool(tool, tool.Description(), schema)
				}
			}
			out = append(out, tool)
		}
		return out, ""
	}
	out := append([]actool.CoreTool(nil), tools...)
	has := map[string]bool{}
	for i, tool := range out {
		has[tool.Name()] = true
		note := ""
		switch tool.Name() {
		case "report_finding":
			note = "\nBy default, the reporter verifies and binds traffic before writing the report. Keep validation commands, key output, existing real traffic IDs and their purposes in evidence for comparison with execution records; do not fetch extra captures just to bind them. Explicit immediate binding remains supported through verified traffic_refs or evidence_hint_id, which reads structured references from the specified local task hint. Any invalid reference rejects the whole report call. TCP/missing-capture findings need no optional references. The returned finding_id and finding_node_id identify the independent record and exploration node respectively."
		case "add_hint", "add_task_hint":
			note = "\nWhen handing off a confirmed finding, retain verified traffic IDs, purposes, notes and order in the corresponding hint's traffic_refs (top level for one hint; the matching hints element for a batch). Explain in text which finding they prove. Never discard existing references and hand off text alone. Unverified candidates are not evidence."
		case "get_finding_traffic", "bind_finding_traffic", "list_findings", "list_task_findings", "node_detail", "get_task_node_detail", "update_finding_report":
			note = findingIDGuidance
		}
		if note != "" {
			out[i] = DecorateTool(tool, tool.Description()+note, tool.InputSchema())
		}
	}
	guidance := ""
	if has["report_finding"] || has["add_task_hint"] || has["add_hint"] {
		guidance = "\n\nOptional traffic-evidence handoff: the reporter normally binds after finding persistence and before reporting. Keep commands, key output, existing real traffic IDs and purposes in evidence, with intent_id inside a task, so the reporter can trace execution. No extra capture lookup is required solely for binding. Auto/Planner must preserve the executor's existing references. Use traffic_refs on add_hint / add_task_hint; report_finding still accepts explicit traffic_refs / evidence_hint_id. Register TCP or other non-HTTP/missing-capture findings normally; never guess IDs or repeat probes just to obtain captures."
		if has["add_task_hint"] && !has["add_hint"] {
			guidance += "\nA platform conversation without task context must hand off using add_task_hint to the existing appropriate task instead of calling report_finding directly. Its task agent registers the finding; verify with list_task_findings."
		}
		if has["prove_goal"] || has["goal_met"] {
			guidance += "\nBefore marking a goal complete, finish reporting/handing off existing evidence. Do not end the task or cancel workers merely because a textual finding exists while handoff remains unfinished. Missing captures do not require waiting or forced capture."
		}
	}
	if has["update_finding_report"] && has["bind_finding_traffic"] && has["get_finding_traffic"] {
		guidance += "\n\nAutomatic pre-report traffic association is enabled. Verify and bind evidence for this triggered finding, then write the report. Obtain explicit finding_id and finding_node_id from report_finding JSON or get_task_node_detail / list_task_findings. Review finding details, its intent execution trace, and current evidence, preferring real IDs already handed off. For HTTP validation with traffic tools available, use traffic_search for candidates and traffic_get to verify each request/response actually supports the finding; domain/time only filter candidates and do not prove association. Bind confirmed references in reproduction order with bind_finding_traffic(finding_id, traffic_refs), assigning baseline / proof / verification / supporting and explaining each purpose. Operate only on this finding; do not duplicate it or probe the target again. After successful binding, call get_finding_traffic again for the latest version and read required bodies. Pass the version actually read as evidence_version to update_finding_report, whose legacy finding_id parameter still takes finding_node_id. Do not append existing bindings again. For TCP, absent capture, unavailable tools, or no exact match, skip automatic binding and write from text/command evidence, explaining the reason. Never invent IDs to complete a capture set. Do not claim failed binding succeeded; retain available evidence and explain why it is unbound."
	}
	if guidance != "" || has["get_finding_traffic"] || has["update_finding_report"] {
		guidance += findingIDGuidance
	}
	return out, guidance
}

func stripTrafficParameters(schema map[string]any) {
	props, _ := schema["properties"].(map[string]any)
	delete(props, "traffic_refs")
	delete(props, "evidence_hint_id")
	if required, ok := schema["required"].([]any); ok {
		kept := required[:0]
		for _, key := range required {
			if key != "traffic_refs" && key != "evidence_hint_id" {
				kept = append(kept, key)
			}
		}
		schema["required"] = kept
	}
	if hints, ok := props["hints"].(map[string]any); ok {
		if items, ok := hints["items"].(map[string]any); ok {
			stripTrafficParameters(items)
		}
	}
}

// HintTrafficSchema is shared by the task-local and cross-task hint tools.
func HintTrafficSchema() map[string]any {
	return map[string]any{"type": "array", "description": "Optional ordered traffic references, verified to support the specific finding in this hint. The recipient can pass evidence_hint_id to report_finding to carry them forward.", "items": obj(map[string]any{"traffic_id": str("Actual traffic ID"), "role": str("baseline / proof / verification / supporting"), "note": str("Which conclusion this traffic supports")}, "traffic_id")}
}

func (t *ToolSet) findingRefsFromHint(hintID int64, explicit []db.TrafficRef) ([]db.TrafficRef, error) {
	if hintID <= 0 {
		return db.NormalizeTrafficRefs(explicit)
	}
	n, err := t.ts.GetNode(hintID) // local store only: inherited hints cannot supply evidence
	if err != nil {
		return nil, err
	}
	if n == nil || n.Kind != db.KindHint {
		return nil, fmt.Errorf("evidence_hint_id=%d must reference a hint in this task; inherited hints cannot directly supply bindings", hintID)
	}
	var payload struct {
		Refs []db.TrafficRef `json:"traffic_refs"`
	}
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, err
	}
	return db.NormalizeTrafficRefs(append(append([]db.TrafficRef{}, explicit...), payload.Refs...))
}
