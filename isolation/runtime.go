package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/mcphttp"
	"github.com/Autumn-27/norma/mcp"
	actool "github.com/Autumn-27/norma/tool"
)

type MCPConnection interface {
	Tools(context.Context) ([]actool.CoreTool, error)
	Close() error
}
type Runtime struct {
	tasks        *actool.Manager
	Tools        map[string]actool.CoreTool
	Custom       func(context.Context, *Custom, *actool.ToolContext) (actool.Result, error)
	ctx          context.Context
	mu           sync.Mutex
	mcps         map[string]MCPConnection
	inputDigests map[string][32]byte
	frozen       bool
}

func NewRuntime(ctx context.Context, custom func(context.Context, *Custom, *actool.ToolContext) (actool.Result, error)) *Runtime {
	r := &Runtime{Tools: map[string]actool.CoreTool{}, Custom: custom, ctx: ctx, mcps: map[string]MCPConnection{}, inputDigests: map[string][32]byte{}}
	for _, tool := range append(actool.DefaultTools(), actool.ShellSessionTools()...) {
		r.Tools[tool.Name()] = tool
	}
	r.tasks, _ = actool.NewManager("isolated-worker")
	for _, t := range []actool.CoreTool{actool.NewTaskOutput(), actool.NewTaskStop(), actool.NewTaskList(), actool.NewMonitor()} {
		r.Tools[t.Name()] = t
	}
	r.Tools["WebFetch"] = actool.NewWebFetch(actool.WebFetchConfig{})
	return r
}
func (r *Runtime) mcpTools(cfg *MCP) ([]actool.CoreTool, error) {
	raw, _ := json.Marshal(cfg)
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:])
	if c := r.mcps[key]; c != nil {
		return c.Tools(r.ctx)
	}
	if len(r.mcps) >= 16 {
		return nil, errors.New("worker MCP connection limit")
	}
	var client MCPConnection
	var err error
	switch cfg.Transport {
	case "stdio":
		client, err = mcp.NewStdioClient(r.ctx, cfg.Name, cfg.Command, cfg.Env, cfg.Args...)
	case "http":
		client, err = mcphttp.New(r.ctx, cfg.Name, cfg.URL, cfg.Env, false)
	case "sse":
		client, err = mcphttp.NewSSE(r.ctx, cfg.Name, cfg.URL, cfg.Env, false)
	default:
		return nil, errors.New("unsupported isolated MCP transport")
	}
	if err != nil {
		return nil, err
	}
	r.mcps[key] = client
	return client.Tools(r.ctx)
}
func (r *Runtime) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := Response{}
	// One run owns this entire container, its manager, MCP clients and private
	// tmpfs. Container removal terminates even detached/untracked descendants.
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if recover() != nil {
			response = Response{Error: "isolated tool panicked"}
		}
		_ = json.NewEncoder(w).Encode(response)
	}()
	request.Body = http.MaxBytesReader(w, request.Body, MaxMessage)
	defer request.Body.Close()
	var req Request
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		response.Error = "invalid tool request"
		return
	}
	if expected := os.Getenv("WORKER_RUN_ID"); expected != "" && (req.RunID != expected || req.Session != os.Getenv("WORKER_SESSION")) {
		response.Error = "worker run identity mismatch"
		return
	}
	if req.Tool == "__snapshot" {
		r.cleanup()
		r.frozen = true
		files, err := ReadFiles("/workspace", 4<<20)
		if err != nil {
			response.Error = err.Error()
			return
		}
		response.Files = files
		response.SnapshotComplete = true
		return
	}
	if req.Tool == "__resume" {
		r.frozen = false
		return
	}
	if r.frozen {
		response.Error = "worker runtime is frozen"
		return
	}
	if r.tasks == nil {
		var err error
		r.tasks, err = actool.NewManager("isolated-worker")
		if err != nil {
			response.Error = "worker task manager unavailable"
			return
		}
	}
	if req.CWD != "" && (!validRelative(req.CWD)) {
		response.Error = "invalid worker directory"
		return
	}
	dir := filepath.Join("/workspace", req.CWD)
	if err := os.MkdirAll(dir, 0700); err != nil {
		response.Error = err.Error()
		return
	}
	changed := map[string][]byte{}
	incoming := map[string][32]byte{}
	for name, data := range req.Uploads {
		digest := sha256.Sum256(data)
		incoming[name] = digest
		if prior, exists := r.inputDigests[name]; !exists || prior != digest {
			changed[name] = data
		}
	}
	if err := WriteFiles("/workspace", changed); err != nil {
		response.Error = err.Error()
		return
	}
	r.inputDigests = incoming
	tc := &actool.ToolContext{WorkingDir: dir, Tasks: r.tasks, OutputDir: filepath.Join(dir, "cmd-output"), MaxOutputChars: 100000}
	var err error
	if req.Custom != nil {
		if r.Custom == nil {
			response.Error = "custom executor unavailable"
			return
		}
		response.Result, err = r.Custom(request.Context(), req.Custom, tc)
	} else {
		tools := r.Tools
		if req.MCP != nil {
			list, e := r.mcpTools(req.MCP)
			if e != nil {
				response.Error = e.Error()
				return
			}
			if req.Tool == "__mcp_raw" {
				raw, _ := json.Marshal(req.MCP)
				digest := sha256.Sum256(raw)
				client, okay := r.mcps[hex.EncodeToString(digest[:])].(*mcphttp.Client)
				if !okay {
					response.Error = "raw MCP is supported only for HTTP batch integrations"
					return
				}
				text, e := client.Call(request.Context(), req.RawMCPTool, req.Input)
				response.Result = actool.Text(text)
				if e != nil {
					response.Error = e.Error()
				}
				return
			}
			if req.Tool == "__mcp_list" {
				for _, tool := range list {
					response.Tools = append(response.Tools, Descriptor{tool.Name(), tool.Description(), tool.InputSchema()})
				}
				return
			}
			tools = map[string]actool.CoreTool{}
			for _, t := range list {
				tools[t.Name()] = t
			}
		}
		tool := tools[req.Tool]
		if tool == nil {
			response.Error = "tool is not registered in the isolated runtime"
			return
		}
		response.Result, err = tool.Call(request.Context(), req.Input, tc)
	}
	if err != nil {
		response.Error = err.Error()
	}
	files, syncErr := ReadFiles("/workspace", 4<<20)
	if syncErr == nil {
		response.Files = files
		response.SnapshotComplete = true
		r.inputDigests = map[string][32]byte{}
		for name, data := range files {
			r.inputDigests[name] = sha256.Sum256(data)
		}
	} else {
		note := actool.Text("[Artifact synchronization limit: inspect large files using worker Read. Workspace is ephemeral.]")
		response.Result.Content = append(response.Result.Content, note.Content...)
	}
}
func validRelative(name string) bool {
	return name != "." && name != ".." && !strings.HasPrefix(name, "/") && !strings.Contains(name, "\\") && filepath.Clean(name) == name && !strings.HasPrefix(name, "../")
}
func (r *Runtime) Close() { r.mu.Lock(); defer r.mu.Unlock(); r.cleanup() }
func (r *Runtime) cleanup() {
	if r.tasks != nil {
		r.tasks.Cleanup()
		r.tasks = nil
	}
	for _, c := range r.mcps {
		_ = c.Close()
	}
	r.mcps = map[string]MCPConnection{}
}

// ForwardUnix is called only inside the worker by docker exec. It has no broker
// credential, API credential or database configuration.
func ForwardUnix(ctx context.Context, input io.Reader, output io.Writer) error {
	transport := &http.Transport{DialContext: unixDial}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}
	request, err := http.NewRequestWithContext(ctx, "POST", "http://worker/call", io.LimitReader(input, MaxMessage+1))
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, err = io.Copy(output, io.LimitReader(response.Body, MaxMessage+1))
	return err
}
