package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewGorm(ctx context.Context, databaseURL string) (*gorm.DB, error) {
	gormLogger := logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})

	orm, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		TranslateError: true,
		Logger:         gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm db: %w", err)
	}

	sqlDB, err := orm.DB()
	if err != nil {
		return nil, fmt.Errorf("gorm db handle: %w", err)
	}

	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping gorm db: %w", err)
	}

	log.Printf("gorm logger ready: level=warn ignoreRecordNotFound=true")

	return orm, nil
}
