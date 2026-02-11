# 打牌记分系统 Go 后端设计（PostgreSQL）

## 1. 技术方案

- 语言与框架：Go + Gin（或 Chi）
- 数据库：PostgreSQL（驱动建议 `pgx`）
- 数据迁移：`goose`
- SQL 访问层：`sqlc`
- 实时通信：WebSocket（房间内玩家/积分/消息实时同步）
- 认证：微信 `code2session` 获取 `openid`，服务端签发 JWT
- 定时任务：Cron（每分钟执行断连检查）

## 2. 功能范围（覆盖当前需求）

- 登录注册：微信登录，首次登录自动创建用户
- 用户管理：昵称、头像、角色（normal/member/admin/super_admin）
- 激活码：激活升级；超级管理员创建激活码
- 房间管理：创建房间（可选密码）、加入、退出、断连 5 分钟视为退出
- 房主能力：踢人、聊天开关（默认关闭）、房主转让（最早入房在线用户）
- 转账：支持负数（反向转账）、可撤回/拒绝，积分自动回滚
- 聊天：全房间可见，频率限制（1 秒 1 条、1 分钟 20 条、最多连续 5 条）
- 战绩：最后一人离开自动结算，支持战绩列表与详情

## 3. API 设计（REST + WebSocket）

### 3.1 认证与用户

- `POST /api/v1/auth/wechat-login`（入参：`code`，返回：`token + user`）
- `GET /api/v1/me`
- `PATCH /api/v1/me`（昵称、头像）
- `POST /api/v1/activation/redeem`
- `POST /api/v1/activation/codes`（仅 super_admin）

### 3.2 房间

- `POST /api/v1/rooms`（创建房间）
- `POST /api/v1/rooms/{roomNo}/join`
- `GET /api/v1/rooms/{roomNo}`
- `POST /api/v1/rooms/{roomNo}/leave`
- `POST /api/v1/rooms/{roomNo}/chat-toggle`
- `POST /api/v1/rooms/{roomNo}/kick`
- `POST /api/v1/rooms/{roomNo}/nickname`
- `POST /api/v1/rooms/{roomNo}/heartbeat`（前台每 30~60 秒上报）

### 3.3 转账与聊天

- `POST /api/v1/rooms/{roomNo}/transfers`
- `POST /api/v1/rooms/{roomNo}/transfers/{id}/action`（`rollback` / `reject`）
- `POST /api/v1/rooms/{roomNo}/messages`
- `GET /api/v1/rooms/{roomNo}/messages?cursor=...`

### 3.4 战绩

- `GET /api/v1/records`
- `GET /api/v1/records/{id}`

### 3.5 WebSocket

- `GET /ws/rooms/{roomNo}`
- 推送事件建议：`member_joined`、`member_left`、`score_changed`、`chat_message`、`system_message`、`room_settled`

## 4. PostgreSQL 数据库设计

### 4.1 枚举类型

- `user_role`: `normal`, `member`, `admin`, `super_admin`
- `room_status`: `active`, `settled`
- `message_type`: `system`, `chat`, `transfer`
- `transfer_status`: `pending`, `rolled_back`, `rejected`

### 4.2 核心表

#### 1) users

- `id bigserial primary key`
- `wx_openid text unique not null`
- `nickname varchar(32) not null`
- `avatar_url text`
- `role user_role not null default 'normal'`
- `active_at timestamptz`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`

#### 2) activation_codes

- `id bigserial primary key`
- `code text unique not null`
- `target_role user_role not null`
- `max_uses int not null default 1`
- `used_count int not null default 0`
- `expires_at timestamptz`
- `created_by bigint references users(id)`
- `created_at timestamptz not null`

#### 3) activation_redeems

- `id bigserial primary key`
- `code_id bigint not null references activation_codes(id)`
- `user_id bigint not null references users(id)`
- `redeemed_at timestamptz not null`
- `unique(code_id, user_id)`

#### 4) rooms

- `id bigserial primary key`
- `room_no varchar(8) unique not null`
- `password_hash text`（空为无密码）
- `owner_user_id bigint not null references users(id)`
- `chat_enabled boolean not null default false`
- `status room_status not null default 'active'`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`
- `closed_at timestamptz`

#### 5) room_members

- `id bigserial primary key`
- `room_id bigint not null references rooms(id)`
- `user_id bigint not null references users(id)`
- `room_nickname varchar(32) not null`
- `score bigint not null default 0`
- `join_seq bigint not null`
- `joined_at timestamptz not null`
- `last_seen_at timestamptz not null`
- `left_at timestamptz`
- `is_online boolean not null default true`
- `unique(room_id, user_id)`

#### 6) score_transfers

- `id bigserial primary key`
- `room_id bigint not null references rooms(id)`
- `from_user_id bigint not null references users(id)`
- `to_user_id bigint not null references users(id)`
- `amount bigint not null check (amount > 0)`
- `status transfer_status not null default 'pending'`
- `created_at timestamptz not null`
- `resolved_at timestamptz`
- `resolved_by_user_id bigint references users(id)`

#### 7) room_messages

- `id bigserial primary key`
- `room_id bigint not null references rooms(id)`
- `sender_user_id bigint references users(id)`（系统消息可空）
- `type message_type not null`
- `content text not null`
- `transfer_id bigint references score_transfers(id)`
- `meta jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null`

#### 8) room_events（审计日志，推荐）

- `id bigserial primary key`
- `room_id bigint not null references rooms(id)`
- `event_type text not null`
- `actor_user_id bigint references users(id)`
- `target_user_id bigint references users(id)`
- `data jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null`

#### 9) match_records

- `id bigserial primary key`
- `room_id bigint not null references rooms(id) unique`
- `settled_at timestamptz not null`
- `created_at timestamptz not null`

#### 10) match_record_players

- `id bigserial primary key`
- `record_id bigint not null references match_records(id)`
- `user_id bigint not null references users(id)`
- `nickname_snapshot varchar(32) not null`
- `final_score bigint not null`
- `unique(record_id, user_id)`

### 4.3 关键索引

- `users(wx_openid)`
- `rooms(room_no)`
- `rooms(status, updated_at desc)`
- `room_members(room_id, is_online)`
- `room_members(room_id, join_seq)`
- `room_messages(room_id, created_at desc)`
- `score_transfers(room_id, status, created_at desc)`
- `match_record_players(user_id, record_id desc)`

## 5. 核心事务与一致性规则

- 创建房间：`rooms + room_members + system_message` 同一事务
- 加入房间：校验密码与状态，`room_members upsert`，记录系统消息
- 转账：锁双方成员行（`FOR UPDATE`），更新积分并写转账记录与消息
- 撤回/拒绝：锁转账行（pending）+ 锁双方成员行，回滚积分，更新消息状态
- 退出房间：置离线；若房主退出，转让给最早在线成员；若无人在线，触发结算
- 断连超时：定时任务扫描 `last_seen_at` 超过 5 分钟的在线成员，执行退出流程

## 6. 聊天限流实现

- 1 秒 1 条：查询本人最近一条聊天消息时间
- 1 分钟 20 条：统计窗口内该用户聊天条数
- 连续 5 条限制：查询房间最近 5 条聊天发送者
- 超限返回业务错误码（例如 `CHAT_RATE_LIMIT`）

## 7. Go 项目目录建议

- `cmd/api/main.go`
- `internal/handler`（HTTP + WS）
- `internal/service`（业务逻辑）
- `internal/repo`（sqlc 生成 + repository 封装）
- `internal/auth`（JWT、微信登录）
- `internal/ws`（房间连接管理、广播）
- `internal/scheduler`（断连清理任务）
- `migrations/`（SQL 迁移）

## 8. 实施优先级（建议）

1. 完成用户、房间、成员、消息、转账、战绩的 migration
2. 实现登录、创建房间、加入房间、房间详情接口
3. 实现转账 + 撤回/拒绝（事务保障）
4. 接入 WebSocket 房间广播
5. 实现断连超时、自动转让房主、自动结算
6. 实现激活码与权限后台能力

## 9. 数据库

1. 从环境变量中读取数据库配置
2. 若需要 redis，说明后我会提供地址，不需要搭建 redis
