package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	AvatarUploadDir  string
	AvatarPublicBase string
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

	avatarPublicBase, err := normalizeAvatarPublicBase(os.Getenv("AVATAR_PUBLIC_BASE_URL"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppAddr:          getEnv("APP_ADDR", ":8080"),
		DatabaseURL:      databaseURL,
		JWTSecret:        jwtSecret,
		TokenTTL:         time.Duration(ttlMinutes) * time.Minute,
		WechatAppID:      os.Getenv("WECHAT_APP_ID"),
		WechatAppSecret:  os.Getenv("WECHAT_APP_SECRET"),
		DisconnectWindow: time.Duration(disconnectMinutes) * time.Minute,
		AvatarUploadDir:  getEnv("AVATAR_UPLOAD_DIR", "build/uploads"),
		AvatarPublicBase: avatarPublicBase,
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

func normalizeAvatarPublicBase(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}

	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("invalid AVATAR_PUBLIC_BASE_URL: %w", err)
	}
	if !u.IsAbs() || strings.ToLower(u.Scheme) != "https" || strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("invalid AVATAR_PUBLIC_BASE_URL: must be absolute https url")
	}

	return strings.TrimRight(v, "/"), nil
}
