// Command api runs the APIMonitory backend: it periodically probes
// configured monitors and serves both the JSON API and the embedded
// frontend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"apimonitory/internal/checker"
	"apimonitory/internal/config"
	"apimonitory/internal/httpapi"
	"apimonitory/internal/scheduler"
	"apimonitory/internal/store"
	"apimonitory/internal/webui"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// appCtx is the app's overall lifetime context: canceling it stops the
	// scheduler loop and aborts any in-flight or background-triggered
	// checks (they're all wired to it).
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	st, err := store.Open(appCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	chk := checker.New(cfg.CheckTimeout)
	sched := scheduler.New(st, chk, cfg)

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		sched.Run(appCtx)
	}()

	mux := httpapi.New(st, sched, appCtx, cfg.CheckInterval, cfg.AppTimezone, cfg.APIToken)

	spa, err := webui.Handler()
	if err != nil {
		slog.Error("failed to build webui handler", "error", err)
		os.Exit(1)
	}
	mux.Handle("/", spa)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("starting server", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped unexpectedly", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutdown signal received")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("server shutdown did not complete cleanly", "error", err)
	}

	cancelApp()
	<-schedulerDone

	slog.Info("shutdown complete")
}
