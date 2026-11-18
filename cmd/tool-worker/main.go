// This binary runs only inside the task container. Its Unix RPC socket is never
// published, and the process has no API, database, broker or signing credentials.
package main

import (
	"context"
	"github.com/Autumn-27/artex/isolation"
	"github.com/Autumn-27/artex/server"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if handled, err := dispatchSupervisor(os.Args[1:]); handled {
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) != 2 {
		log.Fatal("expected serve or call")
	}
	status, err := os.ReadFile("/proc/self/status")
	valid := os.Getuid() == 10001 && os.Geteuid() == 10001 && err == nil && strings.Contains(string(status), "NoNewPrivs:\t1")
	for _, field := range []string{"CapBnd", "CapEff", "CapPrm", "CapInh", "CapAmb"} {
		valid = valid && strings.Contains(string(status), field+":\t0000000000000000")
	}
	if !valid {
		log.Fatal("worker must run as UID10001 with no-new-privileges and every capability set empty")
	}

	if os.Args[1] == "call" {
		if err := isolation.ForwardUnix(context.Background(), os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Args[1] != "serve" {
		log.Fatal("unknown worker mode")
	}
	listener, err := net.Listen("unix", "/tmp/artex-tool-worker.sock")
	if err != nil {
		log.Fatal(err)
	}
	if err = os.Chmod("/tmp/artex-tool-worker.sock", 0600); err != nil {
		log.Fatal(err)
	}
	runtime := isolation.NewRuntime(context.Background(), server.ExecuteIsolatedCustomTool)
	defer runtime.Close()
	service := &http.Server{Handler: runtime, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 30 * time.Second}
	log.Fatal(service.Serve(listener))
}
