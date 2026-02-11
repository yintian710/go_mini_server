package scheduler

import (
	"context"
	"log"
	"time"

	"go_mini_server/internal/service"
)

type DisconnectCleaner struct {
	svc *service.Service
}

func NewDisconnectCleaner(svc *service.Service) *DisconnectCleaner {
	return &DisconnectCleaner{svc: svc}
}

func (cleaner *DisconnectCleaner) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cleaner.svc.CleanupDisconnected(ctx); err != nil {
				log.Printf("cleanup disconnected members failed: %v", err)
			}
		}
	}
}
