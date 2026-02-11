package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go_mini_server/internal/auth"
	"go_mini_server/internal/config"
	"go_mini_server/internal/db"
	"go_mini_server/internal/handler"
	"go_mini_server/internal/scheduler"
	"go_mini_server/internal/service"
	"go_mini_server/internal/ws"

	"github.com/gin-gonic/gin"
)

func main() {
	log.SetOutput(os.Stdout)

	bootStartedAt := time.Now()
	log.Printf("startup: initializing application")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	log.Printf("startup: config loaded app_addr=%s token_ttl=%s disconnect_window=%s", cfg.AppAddr, cfg.TokenTTL, cfg.DisconnectWindow)

	log.Printf("startup: connecting to database")
	gormDB, err := db.NewGorm(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect gorm db failed: %v", err)
	}
	log.Printf("startup: database connection established")

	log.Printf("startup: checking database schema")
	if err := db.EnsureSchema(ctx, gormDB); err != nil {
		log.Fatalf("ensure schema failed: %v", err)
	}
	log.Printf("startup: database schema ready")

	sqlDB, err := gormDB.DB()
	if err != nil {
		log.Fatalf("get gorm sql db failed: %v", err)
	}
	defer sqlDB.Close()

	log.Printf("startup: initializing application services")
	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.TokenTTL)
	wechatClient := auth.NewWechatClient(cfg.WechatAppID, cfg.WechatAppSecret)
	wsHub := ws.NewHub()

	svc := service.New(gormDB, jwtManager, wechatClient, wsHub, cfg.DisconnectWindow)
	wsHandler := ws.NewHandler(wsHub, jwtManager, svc)
	api := handler.NewAPI(svc, jwtManager, wsHandler)

	router := gin.Default()
	router.Use(handler.AllowLocalhostCORS())
	api.RegisterRoutes(router)
	log.Printf("startup: http routes registered")

	cleaner := scheduler.NewDisconnectCleaner(svc)
	go cleaner.Start(ctx)
	log.Printf("startup: disconnect cleaner launched")

	httpServer := &http.Server{
		Addr:              cfg.AppAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("startup: completed in %s", time.Since(bootStartedAt).Truncate(time.Millisecond))
	log.Printf("api server listening on %s", cfg.AppAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen failed: %v", err)
	}
}
