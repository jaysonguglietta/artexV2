package agent

import (
	"context"
	"errors"
	"github.com/Autumn-27/artex/isolation"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	actool "github.com/Autumn-27/norma/tool"
	"iter"
	"log"
	"sync"
)

// isolatedOptions closes the SDK's late-injection paths. In particular, Monitor
// executes commands, and a nil tool slice makes the SDK restore local defaults.
func isolatedOptions(ctx context.Context, opts agentcore.Options) agentcore.Options {
	if isolation.TrustedFixtureMode() {
		return opts
	}
	if opts.Tools == nil {
		opts.Tools = []actool.CoreTool{}
	}
	if len(opts.Tools) > 0 {
		if opts.EnableWebFetch {
			opts.Tools = append(opts.Tools, actool.NewWebFetch(actool.WebFetchConfig{}))
		}
		if !opts.DisableBackgroundTasks && !actool.BackgroundTasksDisabled() {
			opts.Tools = append(opts.Tools, actool.NewTaskOutput(), actool.NewTaskStop(), actool.NewTaskList(), actool.NewMonitor())
		}
	}
	opts.EnableWebFetch = false
	opts.DisableBackgroundTasks = true
	// Search remains a typed provider integration in the control plane; it takes
	// a query, never a target URL or command, and is distinct from worker WebFetch.
	if len(opts.Tools) == 0 {
		opts.EnableWebSearch = false
	}
	names := map[string]bool{"WebFetch": true, "Monitor": true, "TaskOutput": true, "TaskStop": true, "TaskList": true}
	for _, t := range append(actool.DefaultTools(), actool.ShellSessionTools()...) {
		names[t.Name()] = true
	}
	for i, t := range opts.Tools {
		if names[t.Name()] {
			opts.Tools[i] = isolation.Wrap(t, RunInfoFrom(ctx).TaskID)
		}
	}
	return opts
}

type isolatedAgentSession struct {
	*agentcore.Session
	once       sync.Once
	stop       func() bool
	closeRun   func() error
	closeErr   error
	runContext context.Context
}

func (s *isolatedAgentSession) Prompt(ctx context.Context, input string) iter.Seq2[harness.Event, error] {
	return s.Session.Prompt(isolation.WithRunContext(ctx, s.runContext), input)
}
func (s *isolatedAgentSession) Close() error {
	if s.stop != nil {
		s.stop()
	}
	s.finishClose()
	return s.closeErr
}
func (s *isolatedAgentSession) finishClose() {
	s.once.Do(func() {
		var remote error
		if s.closeRun != nil {
			remote = s.closeRun()
			if remote != nil {
				log.Printf("isolated agent cleanup failed: %v", remote)
			}
		}
		var local error
		if s.Session != nil {
			local = s.Session.Close()
		}
		s.closeErr = errors.Join(local, remote)
	})
}
func newAgentSession(ctx context.Context, opts agentcore.Options) *isolatedAgentSession {
	if isolation.RunIDFrom(ctx) == "" {
		ctx = WithRunInfo(ctx, RunInfoFrom(ctx))
	}
	out := &isolatedAgentSession{runContext: ctx, Session: agentcore.NewSession(isolatedOptions(ctx, opts))}
	if !isolation.TrustedFixtureMode() && isolation.Configured() {
		session, _, _, err := isolation.SessionFor(RunInfoFrom(ctx).TaskID, opts.WorkingDir)
		if err == nil {
			_ = isolation.RegisterRunScope(ctx, session)
		}
		out.closeRun = func() error { return isolation.CloseRunContext(ctx) }
		out.stop = context.AfterFunc(ctx, func() { out.finishClose() })
	}
	return out
}
