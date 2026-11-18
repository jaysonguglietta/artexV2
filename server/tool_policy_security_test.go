package server

import (
	"context"
	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
	"testing"
)

// A closed database is a deterministic policy-read failure. It must never
// restore the default executable toolset when the saved binding policy is unknown.
func TestToolPolicyReadFailureRefusesAssembly(t *testing.T) {
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skip("isolated PostgreSQL not configured")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	wireTools(pg, nil)
	defer func() { agent.ToolResolve = nil }()
	if err = pg.Close(); err != nil {
		t.Fatal(err)
	}
	tools := agent.ToolResolve(context.Background(), "worker", actool.DefaultTools())
	if len(tools) != 0 {
		t.Fatal("database failure restored executable defaults")
	}
}
