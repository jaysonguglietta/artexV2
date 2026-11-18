package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// Platform tools for the built-in Auto agent create/update skills, custom tools,
// and MCP servers. They are seeded in tools, bound to Auto by default, and
// injected through hostTools using shared database/filesystem logic.

func (s *Server) platformTools() []actool.CoreTool {
	return []actool.CoreTool{
		s.toolCreateSkill(),
		s.toolUpdateSkillFile(),
		s.toolCreateCustomTool(),
		s.toolUpdateCustomTool(),
		s.toolCreateMCP(),
		s.toolUpdateMCP(),
		s.toolDeleteAssetsByHost(),
	}
}

// platformToolKeys are the tool keys the Auto agent gets bound by default.
var platformToolKeys = []string{
	"create_skill", "update_skill_file",
	"create_custom_tool", "update_custom_tool",
	"create_mcp", "update_mcp",
	"delete_assets_by_host",
}

// ---- assets ----

// toolDeleteAssetsByHost hard-deletes every asset tied to one host (exact match).
// Platform-level (not a per-task tool): operates on the global, cross-task asset store.
func (s *Server) toolDeleteAssetsByHost() actool.CoreTool {
	return wrTool("delete_assets_by_host",
		"Delete assets by exact host: remove the host's domain/subdomain and its services/endpoints.\n"+
			"The host match is lowercase and whitespace-trimmed, never fuzzy or wildcard.\n"+
			"A root domain such as example.com also removes its subdomains and their services/endpoints; a subdomain or IP removes only that host and descendants.\n"+
			"Warning: irreversible hard deletion from the globally shared, cross-task asset store.",
		objSchema(map[string]any{
			"host": strParam("Exact host to delete: domain, subdomain, or IP, such as example.com, a.example.com, or 192.0.2.10"),
		}, "host"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			as := s.assetStore()
			if as == nil {
				return actool.Errorf("Asset store is not initialized"), nil
			}
			var a struct {
				Host string `json:"host"`
			}
			_ = json.Unmarshal(in, &a)
			if strings.TrimSpace(a.Host) == "" {
				return actool.Errorf("host cannot be empty"), nil
			}
			counts, err := as.DeleteByHost(a.Host)
			if err != nil {
				return actool.Errorf("Deletion failed: " + err.Error()), nil
			}
			var total int64
			for _, n := range counts {
				total += n
			}
			return jsonResult(map[string]any{
				"host":            a.Host,
				"deleted":         total,
				"deleted_by_type": counts,
			})
		})
}

// ---- skills ----

func (s *Server) toolCreateSkill() actool.CoreTool {
	return wrTool("create_skill",
		"Create a new skill (writes SKILL.md, following the agentskills.io spec). name: lowercase letters/digits/hyphens.",
		objSchema(map[string]any{
			"name":         strParam("Skill name: starts with a lowercase letter, then letters/digits/hyphens"),
			"description":  strParam("Required skill description: what it does and when to use it"),
			"instructions": strParam("Optional Markdown instructions"),
		}, "name", "description"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct{ Name, Description, Instructions string }
			_ = json.Unmarshal(in, &a)
			if !validSkillName(a.Name) {
				return actool.Errorf("Invalid skill name: lowercase initial letter, letters/digits/hyphens only, at most 64 characters"), nil
			}
			if strings.TrimSpace(a.Description) == "" {
				return actool.Errorf("description is required"), nil
			}
			root, err := os.OpenRoot(s.skillDir)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			defer root.Close()
			// Atomic creation rejects existing files and symlinks. Keep every
			// subsequent operation under the same open filesystem root.
			if err := root.Mkdir(a.Name, 0o700); err != nil {
				if os.IsExist(err) {
					return actool.Errorf("skill already exists: " + a.Name), nil
				}
				return actool.Errorf(err.Error()), nil
			}
			var b strings.Builder
			b.WriteString("---\n")
			fmt.Fprintf(&b, "name: %s\n", a.Name)
			fmt.Fprintf(&b, "description: %s\n", a.Description)
			b.WriteString("---\n")
			if strings.TrimSpace(a.Instructions) != "" {
				b.WriteString(a.Instructions)
			} else {
				fmt.Fprintf(&b, "## %s\n\n1. \n2. \n3. \n", a.Name)
			}
			if err := root.WriteFile(filepath.Join(a.Name, "SKILL.md"), []byte(b.String()), 0o600); err != nil {
				_ = root.RemoveAll(a.Name)
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text("skill created: " + a.Name), nil
		})
}

func (s *Server) toolUpdateSkillFile() actool.CoreTool {
	return wrTool("update_skill_file",
		"Write/overwrite a file within a skill (defaults to SKILL.md). Used to modify skill content or add scripts/references.",
		objSchema(map[string]any{
			"name":    strParam("Skill name"),
			"file":    strParam("Optional relative file path, default SKILL.md; for example scripts/run.py"),
			"content": strParam("Complete file contents"),
		}, "name", "content"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct{ Name, File, Content string }
			_ = json.Unmarshal(in, &a)
			if !validSkillName(a.Name) {
				return actool.Errorf("Invalid skill name"), nil
			}
			root, err := os.OpenRoot(s.skillDir)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			defer root.Close()
			if info, err := root.Stat(a.Name); err != nil {
				return actool.Errorf(err.Error()), nil
			} else if !info.IsDir() {
				return actool.Errorf("skill is not a directory: " + a.Name), nil
			}
			rel := strings.TrimSpace(a.File)
			if rel == "" {
				rel = "SKILL.md"
			}
			clean, msg := skillRelPath(rel)
			if msg != "" {
				return actool.Errorf("Invalid path: " + msg), nil
			}
			full := filepath.Join(a.Name, clean)
			if err := root.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if err := root.WriteFile(full, []byte(a.Content), 0o600); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text("skill file written: " + a.Name + "/" + clean), nil
		})
}

// ---- custom tools ----

type customToolToolInput struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Exec        json.RawMessage `json:"exec"`
	Schema      json.RawMessage `json:"schema"`
	Agents      []string        `json:"agents"`
	Deferred    bool            `json:"deferred"`
	Enabled     *bool           `json:"enabled"`
}

func customToolSchema(keyDesc string) map[string]any {
	return objSchema(map[string]any{
		"key":         strParam(keyDesc),
		"description": strParam("Description supplied to the model"),
		"kind":        strParam("shell | command | script (Python only) | http. shell declares an installed Bash tool and needs no exec/schema; other kinds require exec"),
		"exec":        map[string]any{"type": "object", "description": "Execution specification (not needed for shell): command -> {program,arguments}; script -> {code}; http -> {method,url,headers,body,proxy,use_recording_proxy}. Legacy simple command strings allow grouping only, not shell operators"},
		"schema":      map[string]any{"type": "object", "description": "Parameter JSON Schema: optional for shell/command/script; required for http with properties"},
		"agents":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional bound agent keys"},
		"deferred":    map[string]any{"type": "boolean", "description": "Deferred discovery: only for infrequently used command/script/http tools; ignored for shell"},
		"enabled":     map[string]any{"type": "boolean", "description": "Enabled; default true"},
	}, "key", "kind")
}

func toDBTool(a customToolToolInput) *db.Tool {
	enabled := true
	if a.Enabled != nil {
		enabled = *a.Enabled
	}
	return &db.Tool{
		Key: a.Key, Description: a.Description, Schema: a.Schema, Agents: a.Agents,
		Enabled: enabled, Kind: a.Kind, Exec: a.Exec, Deferred: a.Deferred,
	}
}

func (s *Server) toolCreateCustomTool() actool.CoreTool {
	return wrTool("create_custom_tool", "【Important】 When installing a tool the platform doesn't have, call this tool to register the installed tool with the platform so the platform can invoke it! Creates a custom tool (shell/command/script/http). shell = a bash-environment declaration; only key+description+agents are needed, no exec/schema.",
		customToolSchema("Tool key: lowercase initial letter, then lowercase letters/digits/underscores"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a customToolToolInput
			_ = json.Unmarshal(in, &a)
			a.Key = strings.TrimSpace(a.Key)
			if !reToolKey.MatchString(a.Key) {
				return actool.Errorf("The key must start with a lowercase letter and use only lowercase letters, digits, and underscores"), nil
			}
			if a.Kind != "shell" && a.Kind != "command" && a.Kind != "script" && a.Kind != "http" {
				return actool.Errorf("kind must be shell / command / script / http"), nil
			}
			if a.Kind == "http" && !hasSchemaProps(a.Schema) {
				return actool.Errorf("HTTP tools require a nonempty parameter JSON Schema"), nil
			}
			if exist, _ := s.m.pg.GetTool(a.Key); exist != nil {
				return actool.Errorf("Key already exists: " + a.Key), nil
			}
			if err := s.m.pg.CreateCustomTool(toDBTool(a)); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text("custom tool created: " + a.Key), nil
		})
}

func (s *Server) toolUpdateCustomTool() actool.CoreTool {
	return wrTool("update_custom_tool", "Modify an existing custom tool (by key).",
		customToolSchema("Custom tool key to modify"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a customToolToolInput
			_ = json.Unmarshal(in, &a)
			existing, _ := s.m.pg.GetTool(a.Key)
			if existing == nil || existing.System {
				return actool.Errorf("Only custom tools can be modified: " + a.Key), nil
			}
			if a.Kind != "shell" && a.Kind != "command" && a.Kind != "script" && a.Kind != "http" {
				return actool.Errorf("kind must be shell / command / script / http"), nil
			}
			if a.Kind == "http" && !hasSchemaProps(a.Schema) {
				return actool.Errorf("HTTP tools require a nonempty parameter JSON Schema"), nil
			}
			if err := s.m.pg.UpdateCustomTool(toDBTool(a)); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text("custom tool updated: " + a.Key), nil
		})
}

// ---- MCP ----

type mcpToolInput struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args"`
	Env       json.RawMessage `json:"env"`
	URL       string          `json:"url"`
	Enabled   *bool           `json:"enabled"`
	Insecure  *bool           `json:"insecure"`
}

func mcpSchema(withID bool) map[string]any {
	props := map[string]any{
		"name":      strParam("MCP server name"),
		"transport": strParam("stdio | http / sse"),
		"command":   strParam("stdio executable, such as npx"),
		"args":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Executable argument array"},
		"env":       map[string]any{"type": "object", "description": "Environment variables as {KEY:VALUE}"},
		"url":       strParam("HTTP/SSE URL"),
		"enabled":   map[string]any{"type": "boolean", "description": "Enabled; default true"},
		"insecure":  map[string]any{"type": "boolean", "description": "HTTP: explicitly skip TLS certificate verification for a self-signed endpoint; default false"},
	}
	required := []string{"name", "transport"}
	if withID {
		props["id"] = map[string]any{"type": "integer", "description": "MCP server ID to modify"}
		required = []string{"id", "name", "transport"}
	}
	return objSchema(props, required...)
}

func (a mcpToolInput) toDB() *db.MCPServer {
	enabled := true
	if a.Enabled != nil {
		enabled = *a.Enabled
	}
	insecure := false
	if a.Insecure != nil {
		insecure = *a.Insecure
	}
	return &db.MCPServer{
		ID: a.ID, Name: a.Name, Transport: a.Transport, Command: a.Command,
		Args: a.Args, Env: a.Env, URL: a.URL, Enabled: enabled, Insecure: insecure,
	}
}

func (s *Server) toolCreateMCP() actool.CoreTool {
	return wrTool("create_mcp", "Create an MCP server (stdio/http/sse). After creation, its tools must be authorized per-agent for visibility.",
		mcpSchema(false),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a mcpToolInput
			_ = json.Unmarshal(in, &a)
			a.ID = 0
			if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Transport) == "" {
				return actool.Errorf("name and transport are required"), nil
			}
			id, err := s.m.pg.SaveMCP(a.toDB())
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text(fmt.Sprintf("mcp created: id=%d name=%s", id, a.Name)), nil
		})
}

func (s *Server) toolUpdateMCP() actool.CoreTool {
	return wrTool("update_mcp", "Modify an existing MCP server (by id).",
		mcpSchema(true),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a mcpToolInput
			_ = json.Unmarshal(in, &a)
			if a.ID == 0 {
				return actool.Errorf("id is required"), nil
			}
			if _, err := s.m.pg.SaveMCP(a.toDB()); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text(fmt.Sprintf("mcp updated: id=%d", a.ID)), nil
		})
}
