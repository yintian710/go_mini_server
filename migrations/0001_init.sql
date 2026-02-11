-- +goose Up

CREATE TYPE user_role AS ENUM ('normal', 'member', 'admin', 'super_admin');
CREATE TYPE room_status AS ENUM ('active', 'settled');
CREATE TYPE message_type AS ENUM ('system', 'chat', 'transfer');
CREATE TYPE transfer_status AS ENUM ('pending', 'rolled_back', 'rejected');

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    wx_openid TEXT UNIQUE NOT NULL,
    nickname VARCHAR(32) NOT NULL,
    avatar_url TEXT,
    role user_role NOT NULL DEFAULT 'normal',
    active_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE activation_codes (
    id BIGSERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    target_role user_role NOT NULL,
    max_uses INT NOT NULL DEFAULT 1,
    used_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE activation_redeems (
    id BIGSERIAL PRIMARY KEY,
    code_id BIGINT NOT NULL REFERENCES activation_codes(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (code_id, user_id)
);

CREATE TABLE rooms (
    id BIGSERIAL PRIMARY KEY,
    room_no VARCHAR(8) UNIQUE NOT NULL,
    password_hash TEXT,
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    chat_enabled BOOLEAN NOT NULL DEFAULT false,
    status room_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ
);

CREATE TABLE room_members (
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
);

CREATE TABLE score_transfers (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    from_user_id BIGINT NOT NULL REFERENCES users(id),
    to_user_id BIGINT NOT NULL REFERENCES users(id),
    amount BIGINT NOT NULL CHECK (amount <> 0),
    status transfer_status NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by_user_id BIGINT REFERENCES users(id)
);

CREATE TABLE room_messages (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    sender_user_id BIGINT REFERENCES users(id),
    type message_type NOT NULL,
    content TEXT NOT NULL,
    transfer_id BIGINT REFERENCES score_transfers(id),
    meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE room_events (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL REFERENCES rooms(id),
    event_type TEXT NOT NULL,
    actor_user_id BIGINT REFERENCES users(id),
    target_user_id BIGINT REFERENCES users(id),
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE match_records (
    id BIGSERIAL PRIMARY KEY,
    room_id BIGINT NOT NULL UNIQUE REFERENCES rooms(id),
    settled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE match_record_players (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES match_records(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    nickname_snapshot VARCHAR(32) NOT NULL,
    final_score BIGINT NOT NULL,
    UNIQUE (record_id, user_id)
);

CREATE INDEX idx_users_wx_openid ON users(wx_openid);
CREATE INDEX idx_rooms_room_no ON rooms(room_no);
CREATE INDEX idx_rooms_status_updated_at ON rooms(status, updated_at DESC);
CREATE INDEX idx_room_members_room_online ON room_members(room_id, is_online);
CREATE INDEX idx_room_members_room_join_seq ON room_members(room_id, join_seq);
CREATE INDEX idx_room_messages_room_created ON room_messages(room_id, created_at DESC);
CREATE INDEX idx_score_transfers_room_status_created ON score_transfers(room_id, status, created_at DESC);
CREATE INDEX idx_match_record_players_user_record ON match_record_players(user_id, record_id DESC);

-- +goose Down

DROP INDEX IF EXISTS idx_match_record_players_user_record;
DROP INDEX IF EXISTS idx_score_transfers_room_status_created;
DROP INDEX IF EXISTS idx_room_messages_room_created;
DROP INDEX IF EXISTS idx_room_members_room_join_seq;
DROP INDEX IF EXISTS idx_room_members_room_online;
DROP INDEX IF EXISTS idx_rooms_status_updated_at;
DROP INDEX IF EXISTS idx_rooms_room_no;
DROP INDEX IF EXISTS idx_users_wx_openid;

DROP TABLE IF EXISTS match_record_players;
DROP TABLE IF EXISTS match_records;
DROP TABLE IF EXISTS room_events;
DROP TABLE IF EXISTS room_messages;
DROP TABLE IF EXISTS score_transfers;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS activation_redeems;
DROP TABLE IF EXISTS activation_codes;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS transfer_status;
DROP TYPE IF EXISTS message_type;
DROP TYPE IF EXISTS room_status;
DROP TYPE IF EXISTS user_role;

