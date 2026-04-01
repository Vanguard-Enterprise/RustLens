package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/config"
	httpapi "rustlens/backend/internal/http"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/store"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	postgresStore, err := store.NewPostgresStore(ctx, cfg.PostgresURL)
	if err != nil {
		log.Fatalf("postgres init failed: %v", err)
	}
	defer postgresStore.Close()

	redisStore, err := store.NewRedisStore(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis init failed: %v", err)
	}
	defer redisStore.Close()

	if err := postgresStore.EnsureSchema(ctx); err != nil {
		log.Fatalf("schema migration failed: %v", err)
	}

	shardService := shards.NewService(redisStore, postgresStore, cfg.BotTargetShards, cfg.BotHeartbeatTTL)
	router := httpapi.NewRouter(cfg, postgresStore, redisStore, shardService)
	battleMetricsPoller := battlemetrics.NewPoller(battlemetrics.NewClient(cfg.BattleMetricsAPIKey), postgresStore, redisStore, cfg.BattleMetricsPoll)
	battleMetricsPoller.Start(ctx)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("backend listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown
	cancel()

	ctxShutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctxShutdown); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
