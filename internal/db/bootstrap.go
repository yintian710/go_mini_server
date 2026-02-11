package db

import (
	"context"
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
)

var requiredTables = []string{
	"users",
	"activation_codes",
	"activation_redeems",
	"rooms",
	"room_members",
	"score_transfers",
	"room_messages",
	"room_events",
	"match_records",
	"match_record_players",
}

// EnsureSchema checks core tables and bootstraps schema when missing.
func EnsureSchema(ctx context.Context, orm *gorm.DB) error {
	log.Printf("startup: verifying database schema tables count=%d", len(requiredTables))

	missingTables := make([]string, 0)
	for _, tableName := range requiredTables {
		if !orm.Migrator().HasTable(tableName) {
			missingTables = append(missingTables, tableName)
		}
	}

	if len(missingTables) > 0 {
		log.Printf("startup: missing tables detected: %s", strings.Join(missingTables, ","))
		log.Printf("startup: bootstrapping database schema")
		if err := bootstrapSchema(ctx, orm); err != nil {
			return fmt.Errorf("bootstrap schema for missing tables (%s): %w", strings.Join(missingTables, ","), err)
		}
		log.Printf("startup: schema bootstrap finished")

		for _, tableName := range requiredTables {
			if !orm.Migrator().HasTable(tableName) {
				return fmt.Errorf("table %s still missing after bootstrap", tableName)
			}
		}
	} else {
		log.Printf("startup: all required tables already exist")
	}

	log.Printf("startup: ensuring schema constraints")
	if err := ensureTransferAmountConstraint(ctx, orm); err != nil {
		return fmt.Errorf("ensure score_transfers amount constraint: %w", err)
	}
	log.Printf("startup: schema constraints ready")

	return nil
}

func bootstrapSchema(ctx context.Context, orm *gorm.DB) error {
	sqlStatements := []string{
		`DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'user_role') THEN
        CREATE TYPE user_role AS ENUM ('normal', 'member', 'admin', 'super_admin');
    END IF;
END $$`,
		`DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'room_status') THEN
        CREATE TYPE room_status AS ENUM ('active', 'settled');
    END IF;
END $$`,
		`DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'message_type') THEN
        CREATE TYPE message_type AS ENUM ('system', 'chat', 'transfer');
    END IF;
END $$`,
		`DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'transfer_status') THEN
        CREATE TYPE transfer_status AS ENUM ('pending', 'rolled_back', 'rejected');
    END IF;
END $$`,
		`CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    wx_openid TEXT UNIQUE NOT NULL,
    nickname VARCHAR(32) NOT NULL,
    avatar_url TEXT,
    role user_role NOT NULL DEFAULT 'normal',
    active_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS activation_codes (
    id BIGSERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    target_role user_role NOT NULL,
    max_uses INT NOT NULL DEFAULT 1,
    used_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS activation_redeems (
    id BIGSERIAL PRIMARY KEY,
    code_id BIGINT NOT NULL REFERENCES activation_codes(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (code_id, user_id)
)`,
		`CREATE TABLE IF NOT EXISTS rooms (
    id BIGSERIAL PRIMARY KEY,
    room_no VARCHAR(8) UNIQUE NOT NULL,
    password_hash TEXT,
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    chat_enabled BOOLEAN NOT NULL DEFAULT false,
    status room_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ
)`,
		`CREATE TABLE IF NOT EXISTS room_members (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    room_nickname VARCHAR(32) NOT NULL,
    score BIGINT NOT NULL DEFAULT 0,
    join_seq BIGINT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at TIMESTAMPTZ,
    is_online BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (room_id, user_id)
)`,
		`CREATE TABLE IF NOT EXISTS score_transfers (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    from_user_id BIGINT NOT NULL REFERENCES users(id),
    to_user_id BIGINT NOT NULL REFERENCES users(id),
    amount BIGINT NOT NULL CHECK (amount <> 0),
    status transfer_status NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by_user_id BIGINT REFERENCES users(id)
)`,
		`ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_check`,
		`ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_nonzero_check`,
		`ALTER TABLE score_transfers ADD CONSTRAINT score_transfers_amount_nonzero_check CHECK (amount <> 0)`,
		`CREATE TABLE IF NOT EXISTS room_messages (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    sender_user_id BIGINT REFERENCES users(id),
    type message_type NOT NULL,
    content TEXT NOT NULL,
    transfer_id BIGINT REFERENCES score_transfers(id),
    meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS room_events (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    event_type TEXT NOT NULL,
    actor_user_id BIGINT REFERENCES users(id),
    target_user_id BIGINT REFERENCES users(id),
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS match_records (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL UNIQUE REFERENCES rooms(id),
    settled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`,
		`CREATE TABLE IF NOT EXISTS match_record_players (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES match_records(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    nickname_snapshot VARCHAR(32) NOT NULL,
    final_score BIGINT NOT NULL,
    UNIQUE (record_id, user_id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_users_wx_openid ON users(wx_openid)`,
		`CREATE INDEX IF NOT EXISTS idx_rooms_room_no ON rooms(room_no)`,
		`CREATE INDEX IF NOT EXISTS idx_rooms_status_updated_at ON rooms(status, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_room_members_room_online ON room_members(room_id, is_online)`,
		`CREATE INDEX IF NOT EXISTS idx_room_members_room_join_seq ON room_members(room_id, join_seq)`,
		`CREATE INDEX IF NOT EXISTS idx_room_messages_room_created ON room_messages(room_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_score_transfers_room_status_created ON score_transfers(room_id, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_match_record_players_user_record ON match_record_players(user_id, record_id DESC)`,
	}

	for _, statement := range sqlStatements {
		if err := orm.WithContext(ctx).Exec(statement).Error; err != nil {
			return err
		}
	}

	return nil
}

func ensureTransferAmountConstraint(ctx context.Context, orm *gorm.DB) error {
	if !orm.Migrator().HasTable("score_transfers") {
		return nil
	}

	statements := []string{
		`ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_check`,
		`ALTER TABLE score_transfers DROP CONSTRAINT IF EXISTS score_transfers_amount_nonzero_check`,
		`ALTER TABLE score_transfers ADD CONSTRAINT score_transfers_amount_nonzero_check CHECK (amount <> 0)`,
	}

	for _, statement := range statements {
		if err := orm.WithContext(ctx).Exec(statement).Error; err != nil {
			return err
		}
	}

	return nil
}
