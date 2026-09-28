package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tablescore-api/auth"
	"tablescore-api/config"
	"tablescore-api/handlers"
	"tablescore-api/internal/bgg"
	"tablescore-api/internal/httpapi"
	"tablescore-api/internal/store"
	"tablescore-api/mailer"
	"tablescore-api/models"
	"tablescore-api/services"
	"tablescore-api/ws"

	"github.com/gin-gonic/gin"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	loadDotEnv(".env")
	if os.Getenv("SERVER_PORT") == "" && os.Getenv("PORT") != "" {
		_ = os.Setenv("SERVER_PORT", os.Getenv("PORT"))
	}
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return err
	}
	slog.SetDefault(config.SetupLogger(cfg.IsProd(), cfg.Log.Level))
	orm, err := models.InitDB(cfg)
	if err != nil {
		return err
	}
	database, err := store.NewPostgresStore(orm)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(); err != nil {
		return err
	}
	settings, err := services.NewSettings(orm)
	if err != nil {
		return err
	}
	delivery, err := mailer.New(cfg)
	if err != nil {
		return err
	}
	mail := mailer.Async(delivery, 10*time.Second)
	defer mail.Wait()
	auth.RegisterOAuthProviders(cfg)
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Shutdown()
	go ws.RunBeacons(hub.Context(), hub, settings)
	gin.SetMode(gin.ReleaseMode)
	api, err := httpapi.New(services.NewGameRepository(database, hub), bgg.NewFromEnvironment()).WithFoundation(handlers.Deps{DB: orm, Cfg: cfg, Hub: hub, Settings: settings, Mailer: mail})
	if err != nil {
		return err
	}
	port := fmt.Sprint(cfg.Server.Port)
	server := &http.Server{
		Addr: ":" + port, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	slog.Info("MeppVP API listening", "address", server.Addr)
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		slog.Info("draining connections")
		hub.Shutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown server: %w", err)
	}
	if err := <-result; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func loadDotEnv(fileName string) {
	file, err := os.Open(fileName)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		if _, exists := os.LookupEnv(strings.TrimSpace(key)); !exists {
			_ = os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"`))
		}
	}
}
