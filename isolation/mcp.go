package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

type RemoteMCP struct {
	Config     MCP
	TaskID     int64
	mu         sync.Mutex
	runID      string
	scopes     map[string]bool
	closed     bool
	runContext context.Context
	owned      bool
	closeDone  chan struct{}
	closeErr   error
}

// bind shares the trusted agent identity. Batch integrations without RunInfo own
// an ephemeral identity instead; Close retires every schema/workspace scope used.
func (m *RemoteMCP) bind(ctx context.Context, session string) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ctx, errors.New("remote MCP client is closed")
	}
	incoming := RunIDFrom(ctx)
	if m.runID == "" {
		m.runID = incoming
		if m.runID == "" {
			m.runID = NewRunID()
		}
		m.scopes = map[string]bool{}
		m.owned = incoming == ""
		if m.owned {
			ctx = WithRunID(ctx, m.runID)
		}
		m.runContext = ctx
	}
	if incoming != "" && incoming != m.runID {
		return ctx, errors.New("remote MCP client cannot cross agent runs")
	}
	if err := RegisterRunScope(m.runContext, session); err != nil {
		return ctx, err
	}
	m.scopes[session] = true
	return WithRunContext(ctx, m.runContext), nil
}
func (m *RemoteMCP) Close() error {
	m.mu.Lock()
	if m.closed {
		done := m.closeDone
		m.mu.Unlock()
		<-done
		return m.closeErr
	}
	m.closed = true
	m.closeDone = make(chan struct{})
	owned, runContext := m.owned, m.runContext
	m.mu.Unlock()
	var err error
	if owned {
		err = CloseRunContext(runContext)
	}
	m.mu.Lock()
	m.closeErr = err
	close(m.closeDone)
	m.mu.Unlock()
	if err != nil {
		log.Printf("isolated MCP cleanup failed: %v", err)
	}
	return err
}
func (m *RemoteMCP) session() string {
	if m.TaskID > 0 {
		return "task-" + strconv.FormatInt(m.TaskID, 10)
	}
	return "discovery"
}
func (m *RemoteMCP) Tools(ctx context.Context) ([]actool.CoreTool, error) {
	session := m.session()
	ctx, err := m.bind(ctx, session)
	if err != nil {
		return nil, err
	}
	response, err := Call(ctx, Request{Session: session, RunID: RunIDFrom(ctx), Tool: "__mcp_list", MCP: &m.Config})
	if err != nil {
		return nil, err
	}
	out := []actool.CoreTool{}
	for _, descriptor := range response.Tools {
		d := descriptor
		out = append(out, actool.Build(actool.Spec{Name: d.Name, Description: d.Description, Schema: d.Schema,
			Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
				return permission.Allowed()
			},
			Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
				if tc == nil {
					return actool.Result{}, errors.New("MCP worker context is missing")
				}
				scope, _, _, err := SessionFor(m.TaskID, tc.WorkingDir)
				if err != nil {
					return actool.Errorf(err.Error()), nil
				}
				ctx, err = m.bind(ctx, scope)
				if err != nil {
					return actool.Errorf(err.Error()), nil
				}
				res, err := Execute(ctx, m.TaskID, d.Name, in, tc, nil, &m.Config)
				if err != nil {
					return actool.Errorf(err.Error()), nil
				}
				return res.Result, nil
			},
		}))
	}
	return out, nil
}

// Call preserves full JSON text for trusted batch integrations such as
// ScopeSentry, avoiding the agent-oriented tool-output truncation layer.
func (m *RemoteMCP) Call(ctx context.Context, name string, args any) (string, error) {
	input, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	session := m.session()
	ctx, err = m.bind(ctx, session)
	if err != nil {
		return "", err
	}
	result, err := Call(ctx, Request{Session: session, RunID: RunIDFrom(ctx), Tool: "__mcp_raw", RawMCPTool: name, Input: input, MCP: &m.Config})
	if err != nil {
		return "", err
	}
	if result.Result.IsError {
		return result.Result.Flatten(), errors.New("isolated MCP call failed")
	}
	return result.Result.Flatten(), nil
}
