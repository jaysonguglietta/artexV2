// The broker is a trusted management component. Never publish its port or give
// the API/worker access to its Docker socket or token file mount.
package main

import (
	"context"
	"github.com/Autumn-27/artex/isolation"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	broker, err := isolation.LoadBroker()
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()
	server := &http.Server{Addr: ":8790", Handler: broker, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 6 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print(err)
	}
}
