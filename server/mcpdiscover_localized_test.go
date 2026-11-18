package server

import (
	"context"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// English MCP transport validation coverage. Missing commands/URLs and unknown transport errors return before dialing, so constants and actual error paths are testable without PostgreSQL or network access.
func TestConnectMCPErrorsLocalized(t *testing.T) {
	// Pin the constants.
	for _, c := range []struct{ label, msg string }{
		{"stdio_no_command", errMCPStdioNoCommand},
		{"http_no_url", errMCPHTTPNoURL},
		{"sse_no_url", errMCPSSENoURL},
		{"unknown_transport", errMCPUnknownTransportFmt},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Exercise all four rejection paths before any dial.
	for _, c := range []struct {
		label string
		srv   *db.MCPServer
	}{
		{"stdio_missing_command", &db.MCPServer{Transport: "stdio", Command: ""}},
		{"http_missing_url", &db.MCPServer{Transport: "http", URL: ""}},
		{"sse_missing_url", &db.MCPServer{Transport: "sse", URL: ""}},
		{"unknown_transport_path", &db.MCPServer{Transport: "websocket"}},
	} {
		_, err := connectMCP(context.Background(), c.srv)
		if err == nil {
			t.Fatalf("%s: expected validation error", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}
}
