package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dshills/speccritic/internal/web"
)

func main() {
	config := web.DefaultConfig()
	flag.StringVar(&config.Addr, "addr", config.Addr, "listen address")
	flag.DurationVar(&config.RequestTimeout, "request-timeout", config.RequestTimeout, "request timeout")
	flag.Int64Var(&config.MaxUploadBytes, "max-upload-bytes", config.MaxUploadBytes, "maximum upload size in bytes")
	flag.IntVar(&config.MaxRetainedChecks, "max-retained-checks", config.MaxRetainedChecks, "maximum retained checks")
	flag.DurationVar(&config.RetainedCheckTTL, "retained-check-ttl", config.RetainedCheckTTL, "retained check TTL")
	flag.Parse()

	app, err := web.NewServer(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              config.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       config.RequestTimeout,
		WriteTimeout:      config.RequestTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "INFO: serving SpecCritic web on http://%s\n", config.Addr)
		errCh <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error: shutdown failed: %v\n", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
