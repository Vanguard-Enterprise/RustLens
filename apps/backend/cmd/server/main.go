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
	"rustlens/backend/internal/bmmatch"
	"rustlens/backend/internal/cleanup"
	"rustlens/backend/internal/config"
	"rustlens/backend/internal/events"
	httpapi "rustlens/backend/internal/http"
	"rustlens/backend/internal/outboundhttp"
	"rustlens/backend/internal/platformsync"
	"rustlens/backend/internal/proxycrypto"
	"rustlens/backend/internal/serveranalytics"
	"rustlens/backend/internal/shards"
	"rustlens/backend/internal/steamapi"
	"rustlens/backend/internal/store"
	"rustlens/backend/internal/wipereactscheduler"

	_ "github.com/joho/godotenv/autoload"
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
	proxyCrypto, err := newProxyCrypto(cfg.ProxyEncryptionKey)
	if err != nil {
		log.Fatalf("proxy crypto init failed: %v", err)
	}
	proxyEndpoints, err := initialProxyEndpoints(ctx, postgresStore, proxyCrypto, cfg)
	if err != nil {
		log.Fatalf("outbound proxy load failed: %v", err)
	}
	proxyEnabled := len(proxyEndpoints) > 0

	outboundProvider, err := outboundhttp.NewProvider(proxyEnabled, proxyEndpoints, 20*time.Second)
	if err != nil {
		log.Fatalf("outbound proxy init failed: %v", err)
	}
	outboundProvider.SetAutoDisableCallback(func(redactedEndpoint string) {
		ctxDisable, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		disabled, err := postgresStore.DisableOutboundProxyByRedactedEndpoint(ctxDisable, redactedEndpoint)
		if err != nil {
			log.Printf("[backend] auto-disable proxy %q failed: %v", redactedEndpoint, err)
			return
		}
		if !disabled {
			return
		}

		endpoints, err := initialProxyEndpoints(ctxDisable, postgresStore, proxyCrypto, cfg)
		if err != nil {
			log.Printf("[backend] reload proxies after auto-disable %q failed: %v", redactedEndpoint, err)
			return
		}
		if err := outboundProvider.Replace(len(endpoints) > 0, endpoints); err != nil {
			log.Printf("[backend] apply proxies after auto-disable %q failed: %v", redactedEndpoint, err)
			return
		}
		log.Printf("[backend] auto-disabled outbound proxy %q after >=50%% errors in 10m", redactedEndpoint)
	})
	directProvider, err := outboundhttp.NewProvider(false, nil, 10*time.Second)
	if err != nil {
		log.Fatalf("direct outbound init failed: %v", err)
	}

	shardService := shards.NewService(redisStore, postgresStore, cfg.BotTargetShards, cfg.BotHeartbeatTTL)
	bmMatchClient := bmmatch.NewClient(cfg.BMMatchURL, cfg.BMMatchToken, 10*time.Second)
	platformSyncClient := platformsync.NewClient(cfg.PlatformSyncToken, 10*time.Second)
	eventsHub := events.NewHub()
	eventsBridge := events.NewRedisBridge(redisStore.Client(), eventsHub)
	if err := eventsBridge.Start(ctx); err != nil {
		log.Fatalf("events bridge start failed: %v", err)
	}
	router := httpapi.NewRouter(
		cfg,
		postgresStore,
		redisStore,
		shardService,
		steamapi.NewClient(cfg.SteamAPIKey, directProvider),
		steamapi.NewClient(cfg.SteamAPIKey, directProvider),
		battlemetrics.NewClient(outboundProvider),
		bmMatchClient,
		platformSyncClient,
		outboundProvider,
		proxyCrypto,
		eventsHub,
		eventsBridge,
	)
	battleMetricsPoller := battlemetrics.NewPoller(battlemetrics.NewClient(outboundProvider), postgresStore, redisStore, cfg.BattleMetricsPoll)
	battleMetricsPoller.Start(ctx)
	serverAnalyticsRefresher := serveranalytics.NewRefresher(battlemetrics.NewClient(outboundProvider), postgresStore, redisStore)
	serverAnalyticsRefresher.Start(ctx)
	wipeReactSchedulePoller := wipereactscheduler.NewPoller(postgresStore)
	wipeReactSchedulePoller.Start(ctx)

	cleanupService := cleanup.NewService(postgresStore, cleanup.Config{})
	cleanupService.Start(ctx)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
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

func newProxyCrypto(key string) (*proxycrypto.Crypto, error) {
	if key == "" {
		return nil, nil
	}
	return proxycrypto.New(key)
}

func initialProxyEndpoints(ctx context.Context, postgresStore *store.PostgresStore, proxyCrypto *proxycrypto.Crypto, cfg config.Config) ([]string, error) {
	dbProxies, err := postgresStore.ListEnabledOutboundProxies(ctx)
	if err != nil {
		return nil, err
	}
	if len(dbProxies) == 0 {
		if cfg.ProxyEnabled {
			return cfg.ProxyEndpoints, nil
		}
		return nil, nil
	}
	if proxyCrypto == nil {
		return nil, proxycrypto.ErrKeyRequired
	}

	endpoints := make([]string, 0, len(dbProxies))
	for _, proxy := range dbProxies {
		endpoint, err := proxyCrypto.Decrypt(proxy.Ciphertext, proxy.Nonce)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}
