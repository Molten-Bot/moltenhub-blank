package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/moltenbot000/moltenhub-blank/internal/app"
	"github.com/moltenbot000/moltenhub-blank/internal/hub"
	"github.com/moltenbot000/moltenhub-blank/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	settings := app.DefaultSettings()
	storePath, err := app.ResolveStorePath(settings.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	store, err := app.NewStore(storePath, settings)
	if err != nil {
		log.Fatal(err)
	}
	service := app.NewService(store, hub.NewClient(store.Snapshot().Settings.HubURL))
	if err := service.ConnectFromEnvIfNeeded(ctx); err != nil {
		log.Print(err)
	}
	go service.RunHubLoop(ctx)

	webServer, err := web.New(service)
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{
		Addr:              store.Snapshot().Settings.ListenAddr,
		Handler:           webServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.MarkOffline(shutdownCtx, "server shutdown")
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
