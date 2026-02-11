package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppAddr          string
	DatabaseURL      string
	JWTSecret        string
	TokenTTL         time.Duration
	WechatAppID      string
	WechatAppSecret  string
	DisconnectWindow time.Duration
}

func Load() (Config, error) {
	ttlMinutes, err := getInt("TOKEN_TTL_MINUTES", 10080)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TOKEN_TTL_MINUTES: %w", err)
	}

	disconnectMinutes, err := getInt("DISCONNECT_TIMEOUT_MINUTES", 5)
	if err != nil {
		return Config{}, fmt.Errorf("invalid DISCONNECT_TIMEOUT_MINUTES: %w", err)
	}

	jwtSecret := getEnv("JWT_SECRET", "replace-with-secure-secret")
	databaseURL := getEnv("DATABASE_URL", "postgresql://yintian:ytpostgres@me.yintian.vip:7154/mydb?sslmode=disable")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return Config{
		AppAddr:          getEnv("APP_ADDR", ":8080"),
		DatabaseURL:      databaseURL,
		JWTSecret:        jwtSecret,
		TokenTTL:         time.Duration(ttlMinutes) * time.Minute,
		WechatAppID:      os.Getenv("WECHAT_APP_ID"),
		WechatAppSecret:  os.Getenv("WECHAT_APP_SECRET"),
		DisconnectWindow: time.Duration(disconnectMinutes) * time.Minute,
	}, nil
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.Atoi(v)
}
