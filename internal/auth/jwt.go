package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTManager(secret string, ttl time.Duration) *JWTManager {
	return &JWTManager{secret: []byte(secret), ttl: ttl}
}

func (manager *JWTManager) Generate(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(manager.ttl).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(manager.secret)
}

func (manager *JWTManager) Parse(tokenString string) (int64, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return manager.secret, nil
	})
	if err != nil {
		return 0, fmt.Errorf("parse token: %w", err)
	}

	if !token.Valid {
		return 0, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, fmt.Errorf("invalid claims")
	}

	userIDRaw, ok := claims["sub"]
	if !ok {
		return 0, fmt.Errorf("missing subject")
	}

	userIDFloat, ok := userIDRaw.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid subject type")
	}

	return int64(userIDFloat), nil
}
