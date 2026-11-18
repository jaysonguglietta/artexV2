// Package isolation defines the authenticated control-plane/worker boundary.
package isolation

import (
	"encoding/json"
	actool "github.com/Autumn-27/norma/tool"
)

const MaxMessage = 16 << 20

type MCP struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
}
type Request struct {
	RunID      string            `json:"run_id,omitempty"`
	RawMCPTool string            `json:"raw_mcp_tool,omitempty"`
	Session    string            `json:"session"`
	CWD        string            `json:"cwd,omitempty"`
	Tool       string            `json:"tool"`
	Input      json.RawMessage   `json:"input"`
	MCP        *MCP              `json:"mcp,omitempty"`
	Custom     *Custom           `json:"custom,omitempty"`
	Uploads    map[string][]byte `json:"uploads,omitempty"`
}
type Custom struct {
	Kind   string          `json:"kind"`
	Spec   json.RawMessage `json:"spec"`
	Params map[string]any  `json:"params"`
}
type Descriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}
type Response struct {
	SnapshotComplete bool              `json:"snapshot_complete"`
	Result           actool.Result     `json:"result"`
	Tools            []Descriptor      `json:"tools,omitempty"`
	Files            map[string][]byte `json:"files,omitempty"`
	Error            string            `json:"error,omitempty"`
}
