// Command usherr-relay runs an Usherr Link Relay (ULR): the blind postman
// linked Usherrs talk through. The Usherr project runs one for everyone at
// wss://relay.usherr.app; this is the same protocol for anyone who wants to
// run their own (docs/home/link-protocol.md, "Running your own relay").
//
//	usherr-relay -addr :8787
//	usherr-relay -addr :8443 -tls-cert cert.pem -tls-key key.pem
//
// Without TLS, put it behind a reverse proxy that gives it a wss:// address,
// or use ws:// only on your home network. It keeps everything in memory and
// logs no addresses, keys or messages.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rdborg/mediarium/internal/link/relay"
)

func main() {
	addr := flag.String("addr", envOr("ULR_ADDR", ":8787"), "where to listen (ULR_ADDR)")
	cert := flag.String("tls-cert", os.Getenv("ULR_TLS_CERT"), "TLS certificate file, for wss:// without a proxy (ULR_TLS_CERT)")
	key := flag.String("tls-key", os.Getenv("ULR_TLS_KEY"), "TLS key file (ULR_TLS_KEY)")
	flag.Parse()

	r := relay.New(relay.Options{})
	defer r.Close()
	srv := &http.Server{
		Addr:              *addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(discard{}, "", 0), // no client addresses in the log
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("Usherr Link Relay listening on %s", *addr)
	var err error
	if *cert != "" || *key != "" {
		err = srv.ListenAndServeTLS(*cert, *key)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("usherr-relay: %v", err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
