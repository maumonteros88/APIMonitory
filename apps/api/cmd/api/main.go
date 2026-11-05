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
	"apimonitory/internal/events"
	"apimonitory/internal/httpapi"
	"apimonitory/internal/scheduler"
	"apimonitory/internal/store"
	"apimonitory/internal/webui"
)

// commit identifies the build. Overridden at link time by the release build
// (-ldflags "-X main.commit=<short sha>"); /healthz reports it.
var commit = "dev"

// build is a unique id per release (-X main.build=<id>), so two releases of
// the same commit can be told apart. /healthz reports it too.
var build = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// Registered first so it runs last, after every other cleanup defer
	// below: os.Exit skips deferred calls, so anything that needs to run
	// on a fatal path (closing the store, canceling appCtx) must be
	// deferred before this one and only the exit code set here. Guarded
	// on exitCode != 0 so an unrelated panic unwinds normally (stack
	// trace, exit code 2) instead of being swallowed as a clean exit 0.
	var exitCode int
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		exitCode = 1
		return
	}

	// appCtx is the app's overall lifetime context: canceling it stops the
	// scheduler loop and aborts any in-flight or background-triggered
	// checks (they're all wired to it).
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	st, err := store.Open(appCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		exitCode = 1
		return
	}
	defer st.Close()

	chk := checker.New(cfg.CheckTimeout)

	// hub fans out check results to any open GET /api/events connections.
	// publisher is built before sched: sched needs a Notifier at
	// construction time, and httpapi.New (which would otherwise provide
	// one via *api) itself needs sched already built -- Publisher only
	// needs the store and the hub, so it breaks that cycle.
	hub := events.NewHub()
	publisher := httpapi.NewPublisher(st, cfg.AppTimezone, hub)
	sched := scheduler.New(st, chk, cfg, publisher)

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		sched.Run(appCtx)
	}()

	// maxGap bounds how far apart two scheduled checks can be before
	// buildIncidents treats the gap itself as a break in a down/degraded run
	// (the scheduler wasn't necessarily down for that whole gap, it just
	// didn't run): a retried check (probe + RetryDelay + retry probe) can
	// already take close to 2*CheckInterval on its own, and a tick skipped
	// because a manual check was in flight for that monitor pushes it
	// further still.
	maxGap := 2*cfg.CheckInterval + cfg.RetryDelay + 2*cfg.CheckTimeout

	mux := httpapi.New(st, sched, appCtx, cfg.CheckInterval, cfg.AppTimezone, cfg.APIToken, hub, cfg.SSEHeartbeat, maxGap, commit, build)

	spa, err := webui.Handler()
	if err != nil {
		slog.Error("failed to build webui handler", "error", err)
		exitCode = 1
		return
	}
	mux.Handle("/", spa)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Shutdown (below) waits for active connections to go idle on their
	// own, and an open SSE stream never does. RegisterOnShutdown runs at
	// the start of Shutdown -- unlike cancelApp, which only runs after it
	// returns -- so closing the hub here is what actually lets those
	// connections exit promptly instead of stalling the full
	// shutdownCtx timeout below on every deploy.
	srv.RegisterOnShutdown(hub.Close)

	// Buffered so the goroutine can report a bind/serve failure and exit
	// even if nothing is listening on the other end yet.
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", cfg.ListenAddr, "timezone", cfg.AppTimezone.String())
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// If the server never manages to serve (e.g. the port is already in
	// use), fall through to the same shutdown path instead of leaving the
	// process alive with nothing listening on /healthz.
	select {
	case <-stop:
		slog.Info("shutdown signal received")
	case err := <-serveErr:
		slog.Error("server failed to serve, shutting down", "error", err)
		exitCode = 1
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("server shutdown did not complete cleanly", "error", err)
	}

	cancelApp()
	<-schedulerDone

	slog.Info("shutdown complete")
}
