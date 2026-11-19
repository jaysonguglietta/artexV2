package isolation

import (
	"context"
	"strings"
	"testing"
)

func TestEveryWorkerCreateUsesFiniteLocalLogRotation(t *testing.T) {
	broker := fixtureBroker(t)
	fixture := newFake()
	var created []string
	broker.dockerFn = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "create" {
			created = append([]string(nil), args...)
		}
		return fixture.call(ctx, input, args...)
	}
	defer broker.Close()
	response := brokerRequest(broker, "/v1/call", Request{Session: "task-1", RunID: strings.Repeat("a", 32), Tool: "Ready"})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	driver := ""
	options := map[string]bool{}
	mounts := map[string]bool{}
	for index := 0; index+1 < len(created); index++ {
		if created[index] == "--log-driver" {
			driver = created[index+1]
		}
		if created[index] == "--log-opt" {
			options[created[index+1]] = true
		}
		if created[index] == "--tmpfs" {
			mounts[created[index+1]] = true
		}
	}
	if driver != "local" || !options["max-size=2m"] || !options["max-file=2"] || !options["compress=true"] {
		t.Fatalf("worker log bounds missing: driver=%q options=%v", driver, options)
	}
	if !mounts["/workspace:rw,exec,nosuid,nodev,size=512m,uid=10001,gid=10001"] ||
		!mounts["/tmp:rw,exec,nosuid,nodev,size=128m,uid=10001,gid=10001"] {
		t.Fatal("isolated tool filesystems must permit ordinary CLI execution", mounts)
	}
}
