package isolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

// Reuse a bounded pool; one foreground scan may legitimately take the broker's
// full five-minute deadline. No proxy or redirect may receive broker credentials.
var brokerHTTPClient = &http.Client{
	Timeout:       6 * time.Minute,
	Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 6 * time.Minute, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 8},
	CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("worker redirects are prohibited") },
}

// Configured never treats a legacy host-execution flag as authorization to run
// code in the API process. There is no local fallback on worker failure.
func Configured() bool {
	return os.Getenv("ARTEX_WORKER_URL") != "" && os.Getenv("ARTEX_WORKER_TOKEN_FILE") != ""
}

func Call(ctx context.Context, req Request) (Response, error) {
	return brokerCall(ctx, req, "/v1/call", brokerHTTPClient)
}

// CloseRun uses an independent small control channel, never the busy scan pool.
func CloseRun(ctx context.Context, session, runID string) error {
	if !Configured() {
		return nil
	}
	_, err := brokerCall(ctx, Request{Session: session, RunID: runID}, "/v1/close", brokerCloseClient)
	return err
}

var brokerCloseClient = &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 8}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("worker redirects are prohibited") }}

// LeaseRun can only renew an existing broker-owned run; it cannot create one.
func LeaseRun(ctx context.Context, session, runID string) error {
	if !Configured() {
		return nil
	}
	_, err := brokerCall(ctx, Request{Session: session, RunID: runID}, "/v1/lease", brokerLeaseClient)
	return err
}

var brokerLeaseClient = &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 8 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 8}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("worker redirects are prohibited") }}

func brokerCall(ctx context.Context, req Request, path string, client *http.Client) (Response, error) {
	var out Response
	endpoint, err := url.Parse(os.Getenv("ARTEX_WORKER_URL"))
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && endpoint.Hostname() == "worker-broker")) {
		return out, errors.New("configure an HTTPS worker broker, or the private Compose worker-broker endpoint")
	}
	token, err := os.ReadFile(os.Getenv("ARTEX_WORKER_TOKEN_FILE"))
	if err != nil || len(bytes.TrimSpace(token)) < 32 {
		return out, errors.New("worker broker token is unavailable or too short")
	}
	var bodyValue any = req
	if path == "/v1/close" || path == "/v1/lease" {
		bodyValue = struct {
			Session string `json:"session"`
			RunID   string `json:"run_id"`
		}{req.Session, req.RunID}
	}
	body, err := json.Marshal(bodyValue)
	if err != nil || len(body) > MaxMessage {
		return out, errors.New("worker request exceeds its limit")
	}
	endpoint.Path = path
	r, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	r.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(r)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxMessage+1))
	if err != nil {
		return out, err
	}
	if len(data) > MaxMessage {
		return out, errors.New("worker response exceeds its limit")
	}
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("worker broker refused session %s (%d); configure its operator network policy", req.Session, resp.StatusCode)
	}
	if err = json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, errors.New(out.Error)
	}
	return out, nil
}

// SessionFor is assigned by trusted orchestration, never by model arguments.
func SessionFor(taskID int64, dir string) (string, string, string, error) {
	if taskID > 0 {
		marker := string(filepath.Separator) + "tasks" + string(filepath.Separator) + strconv.FormatInt(taskID, 10)
		clean := filepath.Clean(dir)
		i := strings.LastIndex(clean, marker)
		if i < 0 || (len(clean) > i+len(marker) && clean[i+len(marker)] != filepath.Separator) {
			return "", "", "", errors.New("task workspace identity mismatch")
		}
		root := clean[:i+len(marker)]
		rel, err := filepath.Rel(root, clean)
		if rel == "." {
			rel = ""
		}
		return "task-" + strconv.FormatInt(taskID, 10), root, filepath.ToSlash(rel), err
	}
	if dir == "" || (!strings.Contains(filepath.ToSlash(dir), "/sessions/") && !strings.HasSuffix(filepath.ToSlash(dir), "/tool-tests")) {
		return "", "", "", errors.New("an isolated session requires a workspace")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return "session-" + hex.EncodeToString(sum[:16]), dir, "", nil
}

type remoteTool struct {
	actool.CoreTool
	taskID int64
}

func Wrap(t actool.CoreTool, taskID int64) actool.CoreTool { return &remoteTool{t, taskID} }
func (t *remoteTool) Call(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	if tc == nil {
		return actool.Errorf("Isolated worker requires a task context"), nil
	}
	out, err := Execute(ctx, t.taskID, t.Name(), in, tc, nil, nil)
	if err != nil {
		return actool.Errorf("Isolated worker: " + err.Error()), nil
	}
	return out.Result, nil
}
func Execute(ctx context.Context, taskID int64, name string, in json.RawMessage, tc *actool.ToolContext, custom *Custom, mcp *MCP) (Response, error) {
	if tc == nil {
		return Response{}, errors.New("worker context is missing")
	}
	session, root, cwd, err := SessionFor(taskID, tc.WorkingDir)
	if err != nil {
		return Response{}, err
	}
	runID := RunIDFrom(ctx)
	if runID == "" {
		runID = NewRunID()
		ctx = WithRunID(ctx, runID)
		defer func() {
			if err := CloseRunContext(ctx); err != nil {
				log.Printf("isolated one-shot cleanup failed: %v", err)
			}
		}()
	}
	if err := RegisterRunScope(ctx, session); err != nil {
		return Response{}, err
	}
	if !validRunID(runID) {
		return Response{}, errors.New("invalid worker run identity")
	}
	// The API's private paths are not mounted into the worker. Translate only the
	// task workspace prefix, including paths embedded in command strings.
	mapped := bytes.ReplaceAll(in, []byte(filepath.ToSlash(root)), []byte("/workspace"))
	uploads, err := ReadFiles(root, 4<<20)
	if err != nil && !os.IsNotExist(err) {
		return Response{}, err
	}
	prefixed := map[string][]byte{}
	for k, v := range uploads {
		prefixed[k] = v
	}
	if custom != nil {
		raw, _ := json.Marshal(custom)
		raw = bytes.ReplaceAll(raw, []byte(filepath.ToSlash(root)), []byte("/workspace"))
		var mappedCustom Custom
		if err = json.Unmarshal(raw, &mappedCustom); err != nil {
			return Response{}, err
		}
		custom = &mappedCustom
	}
	res, err := Call(ctx, Request{RunID: runID, Session: session, CWD: cwd, Tool: name, Input: mapped, Custom: custom, MCP: mcp, Uploads: prefixed})
	if err != nil {
		return res, err
	}
	if !res.SnapshotComplete {
		return res, nil
	}
	if err = ApplySnapshot(root, prefixed, res.Files); err != nil {
		return res, err
	}
	return res, nil
}
