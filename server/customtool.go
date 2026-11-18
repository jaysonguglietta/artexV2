package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/isolation"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// Custom tools are stored as non-system rows and injected through hostTools with binding filters. Production command, Python and HTTP calls go through the isolated worker broker; only the isolated runtime and trusted Go fixtures execute the local implementations below.

// Custom-tool CRUD.

type customToolReq struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Agents      []string        `json:"agents"`
	Enabled     bool            `json:"enabled"`
	Kind        string          `json:"kind"` // command | script | http
	Exec        json.RawMessage `json:"exec"`
	Deferred    bool            `json:"deferred"`
}

var reToolKey = reAgentKey // Same key grammar as agents: lowercase initial letter followed by lowercase letters, digits or underscores.

// User-facing custom-tool validation responses and model-facing schema/result narrative use English; preserve payload data and protocol field names.
const (
	errCustomToolKeyFormat          = "key must start with a lowercase letter and may contain only lowercase letters, digits, and underscores"
	errCustomToolKindInvalid        = "The kind value must be one of command, script, http, or shell"
	errCustomToolHTTPSchemaRequired = "The http tool requires a parameter JSON Schema (it cannot be empty)"
	errCustomToolKeyExists          = "This key already exists (built-in or custom tool)"
	errCustomToolEditCustomOnly     = "Only custom tools can be edited"
	errCustomToolBadBody            = "The request body is invalid"
	errCustomToolShellNoExec        = "A shell-type tool is a bash environment declaration, so there is nothing to execute"
	errCustomToolUnknownKindPrefix  = "Unknown tool type: "
)

func (s *Server) pgCreateCustomTool(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req customToolReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if !reToolKey.MatchString(req.Key) {
		writeErr(w, 400, errCustomToolKeyFormat)
		return
	}
	if req.Kind != "command" && req.Kind != "script" && req.Kind != "http" && req.Kind != "shell" {
		writeErr(w, 400, errCustomToolKindInvalid)
		return
	}
	if req.Kind == "http" && !hasSchemaProps(req.Schema) {
		writeErr(w, 400, errCustomToolHTTPSchemaRequired)
		return
	}
	if exist, _ := pg.GetTool(req.Key); exist != nil {
		writeErr(w, 409, errCustomToolKeyExists)
		return
	}
	if err := pg.CreateCustomTool(&db.Tool{
		Key: req.Key, Description: req.Description, Schema: req.Schema, Agents: req.Agents,
		Enabled: req.Enabled, Kind: req.Kind, Exec: req.Exec, Deferred: req.Deferred,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"key": req.Key})
}

func (s *Server) pgUpdateCustomTool(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	key := r.PathValue("key")
	existing, err := pg.GetTool(key)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if existing == nil || existing.System {
		writeErr(w, 400, errCustomToolEditCustomOnly)
		return
	}
	var req customToolReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Kind != "command" && req.Kind != "script" && req.Kind != "http" && req.Kind != "shell" {
		writeErr(w, 400, errCustomToolKindInvalid)
		return
	}
	if req.Kind == "http" && !hasSchemaProps(req.Schema) {
		writeErr(w, 400, errCustomToolHTTPSchemaRequired)
		return
	}
	if err := pg.UpdateCustomTool(&db.Tool{
		Key: key, Description: req.Description, Schema: req.Schema, Agents: req.Agents,
		Enabled: req.Enabled, Kind: req.Kind, Exec: req.Exec, Deferred: req.Deferred,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) pgDeleteCustomTool(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	key := r.PathValue("key")
	if err := pg.DeleteCustomTool(key); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": key})
}

// testToolReq is a dry-run request from the editor: run the given (possibly unsaved)
// exec spec with sample params, without persisting the tool. Same executor path as a
// real tool call — it runs arbitrary command/script/http on the server, which the
// custom-tool feature already allows, so no new capability is granted.
type testToolReq struct {
	Kind   string          `json:"kind"` // command | script | http
	Exec   json.RawMessage `json:"exec"`
	Params map[string]any  `json:"params"`
}

// pgTestCustomTool executes an exec spec once and returns its raw output + error
// flag, so the editor can debug a tool before saving it. Per-kind timeouts still
// apply from the exec spec (with defaults); the outer ceiling is a hard backstop.
func (s *Server) pgTestCustomTool(w http.ResponseWriter, r *http.Request) {
	if !isolation.ExecutionEnabled() {
		writeErr(w, 403, "Custom tool execution requires the isolated worker broker")
		return
	}

	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req testToolReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, errCustomToolBadBody)
		return
	}
	params := req.Params
	if params == nil {
		params = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	testDir := filepath.Join(s.m.dir, "tool-tests")
	if err := os.MkdirAll(testDir, 0700); err != nil {
		writeErr(w, 500, "Cannot create isolated test workspace")
		return
	}
	tc := &actool.ToolContext{WorkingDir: testDir} // run in the project dir, like a real call
	var res actool.Result
	switch req.Kind {
	case "command":
		res, _ = s.runCommandTool(ctx, req.Exec, params, tc)
	case "script":
		res, _ = s.runScriptTool(ctx, "test", req.Exec, params, tc)
	case "http":
		res, _ = s.runHTTPTool(ctx, req.Exec, params, tc)
	case "shell":
		writeErr(w, 400, errCustomToolShellNoExec)
		return
	default:
		writeErr(w, 400, errCustomToolUnknownKindPrefix+req.Kind)
		return
	}
	writeJSON(w, 200, map[string]any{"output": res.Flatten(), "is_error": res.IsError})
}

// Python interpreter detection and saved overrides.

const settingPythonInterp = "python_interpreter"

// detectPython finds a python interpreter absolute path (python3 preferred).
func detectPython() string {
	for _, c := range []string{"python3", "python"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// pythonInterpreter resolves the interpreter: user-set > stored auto-detect > live
// detect. "" only when truly none found.
func (s *Server) pythonInterpreter() string {
	if s.m.pg != nil {
		if v, ok, _ := s.m.pg.GetSetting(settingPythonInterp); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return detectPython()
}

// seedPythonInterpreter stores the auto-detected interpreter on startup if unset
// (never clobbers a user-set value).
func (s *Server) seedPythonInterpreter() {
	if v, ok, _ := s.m.pg.GetSetting(settingPythonInterp); ok && strings.TrimSpace(v) != "" {
		return
	}
	if p := detectPython(); p != "" {
		_ = s.m.pg.SetSetting(settingPythonInterp, p)
		log.Printf("[custom-tool] auto-detected python interpreter: %s", p)
	}
}

// Execution specifications.

type commandExec struct {
	Command   string   `json:"command"`
	Program   string   `json:"program,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
	TimeoutMs int      `json:"timeout_ms"`
}
type scriptExec struct {
	Code      string `json:"code"`
	TimeoutMs int    `json:"timeout_ms"`
}
type httpExec struct {
	Method            string            `json:"method"`
	URL               string            `json:"url"`
	Headers           map[string]string `json:"headers"`
	Body              string            `json:"body"`
	TimeoutMs         int               `json:"timeout_ms"`
	Proxy             string            `json:"proxy"`
	UseRecordingProxy bool              `json:"use_recording_proxy"`
}

func timeoutOr(ms, def int) time.Duration {
	if ms <= 0 {
		return time.Duration(def) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// Common tool construction.

// customTools builds CoreTools for every user-defined (system=false) tool row.
// shell-kind tools are environment hints only — they surface in the Bash tool
// description via ToolResolve and do NOT create callable tool entries here.
func (s *Server) customTools() ([]actool.CoreTool, error) {
	rows, err := s.m.pg.ListCustomTools()
	if err != nil {
		return nil, err
	}
	out := make([]actool.CoreTool, 0, len(rows))
	for _, t := range rows {
		if t.Kind == "shell" {
			continue // shell hints are handled by ToolResolve → Bash description
		}
		out = append(out, s.buildCustomTool(t))
	}
	return out, nil
}

// buildCustomTool converts a saved row into a CoreTool. Empty schemas receive an args:string wrapper for template parameters.
func (s *Server) buildCustomTool(t *db.Tool) actool.CoreTool {
	schema := ensureSchema(t.Schema)
	key, kind, execRaw := t.Key, t.Kind, t.Exec
	run := func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
		var params map[string]any
		if len(in) > 0 {
			_ = json.Unmarshal(in, &params)
		}
		if params == nil {
			params = map[string]any{}
		}
		switch kind {
		case "command":
			return s.runCommandTool(ctx, execRaw, params, tc)
		case "script":
			return s.runScriptTool(ctx, key, execRaw, params, tc)
		case "http":
			return s.runHTTPTool(ctx, execRaw, params, tc)
		default:
			return actool.Errorf("Unknown custom tool kind: " + kind), nil
		}
	}
	return actool.Build(actool.Spec{
		Name: key, Description: t.Description, Schema: schema,
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: run,
	})
}

// hasSchemaProps reports whether raw is a JSON-Schema object with ≥1 property.
// http tools require an explicit schema (the auto {args} shell can't name the
// {param} placeholders in URL/headers/body), so an empty schema is rejected.
func hasSchemaProps(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	props, _ := m["properties"].(map[string]any)
	return len(props) > 0
}

// ensureSchema returns the tool's schema, or a thin {args:string} when none given.
func ensureSchema(raw json.RawMessage) map[string]any {
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	props, _ := m["properties"].(map[string]any)
	if len(props) > 0 {
		return m
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{"type": "string", "description": "Command/arguments (free text)"},
		},
	}
}

// Command execution with a fixed program and literal argument interpolation, inside the worker in production.

func (s *Server) runCommandTool(ctx context.Context, execRaw json.RawMessage, params map[string]any, tc *actool.ToolContext) (actool.Result, error) {
	if !s.isolatedRuntime && !isolation.TrustedFixtureMode() {
		return s.remoteCustomTool(ctx, "command", execRaw, params, tc)
	}
	var spec commandExec
	_ = json.Unmarshal(execRaw, &spec)
	words := append([]string{spec.Program}, spec.Arguments...)
	if spec.Program == "" {
		var err error
		words, err = commandWords(spec.Command)
		if err != nil {
			return actool.Errorf(err.Error()), nil
		}
	}
	if len(words) == 0 || strings.ContainsAny(words[0], "{}") {
		return actool.Errorf("Configure a fixed executable and separate arguments"), nil
	}
	for i := 1; i < len(words); i++ {
		words[i] = renderTemplate(words[i], params, identity)
	}
	workDir := s.m.dir
	var sessionEnv []string
	if tc != nil {
		if tc.WorkingDir != "" {
			workDir = tc.WorkingDir
		}
		sessionEnv = tc.Env
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutOr(spec.TimeoutMs, 120000))
	defer cancel()
	c := exec.CommandContext(runCtx, words[0], words[1:]...)
	c.Dir = workDir
	c.Env = toolEnvironment(sessionEnv, workDir)
	c.WaitDelay = 2 * time.Second
	out := &boundedToolOutput{cancel: cancel}
	c.Stdout = out
	c.Stderr = out
	err := c.Run()
	body := out.String()
	if err != nil {
		body += "\n[execution failed: " + err.Error() + "]"
	}
	if out.exceeded {
		return actool.Errorf(body + "\n[output limit exceeded]"), nil
	}
	if err != nil {
		return actool.Errorf(body), nil
	}
	return actool.Text(actool.Capture(tc, body)), nil
}

// Python execution through a temporary file, JSON stdin and operational environment.

func (s *Server) runScriptTool(ctx context.Context, key string, execRaw json.RawMessage, params map[string]any, tc *actool.ToolContext) (actool.Result, error) {
	if !s.isolatedRuntime && !isolation.TrustedFixtureMode() {
		return s.remoteCustomTool(ctx, "script", execRaw, params, tc)
	}
	var spec scriptExec
	_ = json.Unmarshal(execRaw, &spec)
	if strings.TrimSpace(spec.Code) == "" {
		return actool.Errorf("Script code is empty"), nil
	}
	interp := s.pythonInterpreter()
	if interp == "" {
		return actool.Errorf("No Python interpreter is configured or detected; set one in system settings"), nil
	}
	workDir := s.m.dir
	var sessionEnv []string
	if tc != nil {
		if tc.WorkingDir != "" {
			workDir = tc.WorkingDir
		}
		sessionEnv = tc.Env
	}
	body, err := execPython(ctx, interp, key, spec.Code, params, workDir, sessionEnv, timeoutOr(spec.TimeoutMs, 120000))
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	return actool.Text(actool.Capture(tc, body)), nil
}

// execPython writes the code to a temp .py under workDir/.tools, runs it via interp
// with the params JSON on stdin + scalar params mirrored to TOOL_<NAME> env, and
// returns combined stdout+stderr (with a timeout/exit note). Standalone + testable.
func execPython(ctx context.Context, interp, key, code string, params map[string]any, workDir string, sessionEnv []string, timeout time.Duration) (string, error) {
	toolsDir := filepath.Join(workDir, ".tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(toolsDir, key+"-*.py")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(code); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(runCtx, interp, tmp)
	c.Dir = workDir
	c.Env = toolEnvironment(sessionEnv, workDir)
	for k, v := range params { // Mirror scalar parameters as TOOL_<NAME> environment variables.
		if sv, ok := scalarStr(v); ok {
			c.Env = append(c.Env, "TOOL_"+strings.ToUpper(k)+"="+sv)
		}
	}
	pj, _ := json.Marshal(params)
	c.Stdin = bytes.NewReader(pj) // Pass the parameter JSON on stdin.
	out := &boundedToolOutput{cancel: cancel}
	c.Stdout = out
	c.Stderr = out
	c.WaitDelay = 2 * time.Second
	err = c.Run()
	body := out.String()
	if out.exceeded {
		body += "\n[output limit exceeded]"
	}
	if runCtx.Err() == context.DeadlineExceeded {
		body += "\n... [terminated on timeout] ..."
	} else if err != nil {
		body += "\n[exit: " + err.Error() + "]"
	}
	return body, nil
}

// HTTP execution; workers use direct target requests and cannot reach the API recording proxy.

func (s *Server) runHTTPTool(ctx context.Context, execRaw json.RawMessage, params map[string]any, tc *actool.ToolContext) (actool.Result, error) {
	if !s.isolatedRuntime && !isolation.TrustedFixtureMode() {
		return s.remoteCustomTool(ctx, "http", execRaw, params, tc)
	}
	var spec httpExec
	_ = json.Unmarshal(execRaw, &spec)
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method == "" {
		method = "GET"
	}
	rawURL := renderTemplate(spec.URL, params, identity)
	if strings.TrimSpace(rawURL) == "" {
		return actool.Errorf("HTTP URL is empty"), nil
	}
	var bodyReader io.Reader
	if spec.Body != "" {
		bodyReader = strings.NewReader(renderTemplate(spec.Body, params, identity))
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutOr(spec.TimeoutMs, 30000))
	defer cancel()
	req, err := http.NewRequestWithContext(runCtx, method, rawURL, bodyReader)
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, renderTemplate(v, params, identity))
	}
	client := &http.Client{Timeout: timeoutOr(spec.TimeoutMs, 30000)}
	if tr := s.httpProxyTransport(spec); tr != nil {
		client.Transport = tr
	}
	resp, err := client.Do(req)
	if err != nil {
		return actool.Errorf("Request failed: " + err.Error()), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out := map[string]any{"status": resp.StatusCode, "body": string(respBody)}
	b, _ := json.Marshal(out)
	return actool.Text(actool.Capture(tc, string(b))), nil
}

// httpProxyTransport builds a Transport for the http tool's proxy config, or nil
// (direct). use_recording_proxy routes through the recording proxy + trusts its CA.
func (s *Server) httpProxyTransport(spec httpExec) *http.Transport {
	proxyStr := strings.TrimSpace(spec.Proxy)
	var caFile string
	if spec.UseRecordingProxy {
		if addr := s.m.ProxyAddr(); addr != "" {
			proxyStr = "http://" + addr
			caFile = s.m.ProxyCACert()
		}
	}
	if proxyStr == "" {
		return nil
	}
	pu, err := url.Parse(proxyStr)
	if err != nil {
		return nil
	}
	tr := &http.Transport{Proxy: http.ProxyURL(pu)}
	if caFile != "" {
		if pem, err := os.ReadFile(caFile); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				tr.TLSClientConfig = &tls.Config{RootCAs: pool}
			}
		}
	}
	return tr
}

// ---------- helpers ----------

func identity(s string) string { return s }

// shellQuote single-quotes a value for safe shell interpolation.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// renderTemplate replaces {name} placeholders with each param's rendered value.
var templateParameter = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func renderTemplate(tmpl string, params map[string]any, quote func(string) string) string {
	return templateParameter.ReplaceAllStringFunc(tmpl, func(match string) string {
		if v, ok := params[match[1:len(match)-1]]; ok {
			return quote(valToStr(v))
		}
		return match
	})
}

// valToStr renders a param value: scalars as-is, arrays/objects as compact JSON.
func valToStr(v any) string {
	if sv, ok := scalarStr(v); ok {
		return sv
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// scalarStr returns (string, true) for scalar values, ("", false) for arrays/objects.
func scalarStr(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return fmt.Sprintf("%t", x), true
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x)), true
		}
		return fmt.Sprintf("%g", x), true
	case nil:
		return "", true
	default:
		return "", false
	}
}

// commandWords accepts quotes for grouping, but never invokes a shell. Shell
// operators/expansions in legacy templates require migration to a reviewed script.
func commandWords(input string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range input {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			if quote == 0 {
				quote = r
				started = true
				continue
			}
			if quote == r {
				quote = 0
				continue
			}
		}
		if quote == 0 && (r == ' ' || r == '\t') {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
			continue
		}
		if strings.ContainsRune("$`|&;<>\n\r()", r) {
			return nil, fmt.Errorf("Shell syntax is disabled in command tools; use executable/arguments or a reviewed script tool")
		}
		word.WriteRune(r)
		started = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("Unclosed quote or escape in command template")
	}
	if started {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("Command is empty")
	}
	return words, nil
}

// Tool subprocesses receive only operational variables, never service secrets.
func toolEnvironment(session []string, workDir string) []string {
	allowed := map[string]bool{"PATH": true, "LANG": true, "LC_ALL": true, "SYSTEMROOT": true, "SystemRoot": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true, "SSL_CERT_FILE": true, "REQUESTS_CA_BUNDLE": true, "CURL_CA_BUNDLE": true, "NODE_EXTRA_CA_CERTS": true}
	env := map[string]string{"HOME": workDir, "USERPROFILE": workDir}
	for _, pair := range append(os.Environ(), session...) {
		key, value, ok := strings.Cut(pair, "=")
		if ok && allowed[key] {
			env[key] = value
		}
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

type boundedToolOutput struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedToolOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 1 << 20
	if b.buf.Len()+len(p) > limit {
		remaining := limit - b.buf.Len()
		b.buf.Write(p[:remaining])
		b.exceeded = true
		b.cancel()
		return len(p), nil
	}
	return b.buf.Write(p)
}
func (b *boundedToolOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

// remoteCustomTool never falls back to the API's process or filesystem.
func (s *Server) remoteCustomTool(ctx context.Context, kind string, spec json.RawMessage, params map[string]any, tc *actool.ToolContext) (actool.Result, error) {
	res, err := isolation.Execute(ctx, agent.RunInfoFrom(ctx).TaskID, "__custom", nil, tc, &isolation.Custom{Kind: kind, Spec: spec, Params: params}, nil)
	if err != nil {
		return actool.Errorf("Isolated worker: " + err.Error()), nil
	}
	return res.Result, nil
}

// ExecuteIsolatedCustomTool is wired only into the task-runtime binary. It has
// no database handle, proxy credentials or control-plane configuration.
func ExecuteIsolatedCustomTool(ctx context.Context, c *isolation.Custom, tc *actool.ToolContext) (actool.Result, error) {
	s := &Server{m: &Manager{dir: tc.WorkingDir}, isolatedRuntime: true}
	switch c.Kind {
	case "command":
		return s.runCommandTool(ctx, c.Spec, c.Params, tc)
	case "script":
		return s.runScriptTool(ctx, "isolated", c.Spec, c.Params, tc)
	case "http":
		var spec httpExec
		if json.Unmarshal(c.Spec, &spec) != nil || spec.UseRecordingProxy || spec.Proxy != "" {
			return actool.Errorf("Use direct target HTTP inside the worker; control-plane proxies are prohibited"), nil
		}
		return s.runHTTPTool(ctx, c.Spec, c.Params, tc)
	default:
		return actool.Errorf("Unknown isolated custom tool kind"), nil
	}
}
