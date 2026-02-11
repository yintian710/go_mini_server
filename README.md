# go_mini_server

打牌记分系统后端（Go + Gin + PostgreSQL + WebSocket + GORM）。

## 已实现能力

- 微信登录（开发环境使用 mock openid）+ JWT 鉴权
- 账号登录（非微信）+ JWT 鉴权
- 用户资料读写
- 激活码创建（super_admin）与兑换
- 房间创建/加入/详情/退出/踢人/心跳/房间昵称/聊天开关
- 转账（支持负数，不翻转转账双方）+ 撤回/拒绝（积分回滚）
- 聊天消息发送与分页查询（含限流）
- 房间 WebSocket 广播（成员变化、聊天、积分变化、结算）
- 最后一人离开自动结算并写入战绩
- 每分钟断连扫描（5 分钟未心跳自动离开）
- 业务层统一使用 `GORM`（含事务与行级锁）

## 目录

- `cmd/api/main.go`：程序入口
- `internal/config`：配置读取
- `internal/db`：GORM 数据库初始化
- `internal/auth`：JWT 与微信登录适配
- `internal/service`：核心业务逻辑与事务
- `internal/handler`：HTTP API
- `internal/ws`：WebSocket 房间广播
- `internal/scheduler`：定时断连清理
- `migrations/0001_init.sql`：数据库初始化 migration

## 环境变量

- `DATABASE_URL`：必填，例如 `postgres://user:pass@127.0.0.1:5432/game?sslmode=disable`
- `APP_ADDR`：默认 `:8080`
- `JWT_SECRET`：默认 `replace-with-secure-secret`
- `TOKEN_TTL_MINUTES`：默认 `10080`（7 天）
- `DISCONNECT_TIMEOUT_MINUTES`：默认 `5`
- `WECHAT_APP_ID`：可选
- `WECHAT_APP_SECRET`：可选

> 未配置微信参数时，`/auth/wechat-login` 会根据 code 生成 mock openid，便于本地联调。

## 运行

1. 初始化数据库（建议使用 goose 执行 `migrations/0001_init.sql`）
2. 启动服务：

```bash
go run ./cmd/api
```

> 启动时会自动检查核心表是否存在；若缺失会自动建表（含索引与枚举类型）。

## 主要 API 前缀

- `POST /api/v1/auth/wechat-login`
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET/PATCH /api/v1/me`
- `POST /api/v1/activation/redeem`
- `POST /api/v1/activation/codes`
- `POST /api/v1/rooms`
- `POST /api/v1/rooms/{roomNo}/join`
- `GET /api/v1/rooms/{roomNo}`
- `POST /api/v1/rooms/{roomNo}/leave`
- `POST /api/v1/rooms/{roomNo}/chat-toggle`
- `POST /api/v1/rooms/{roomNo}/kick`
- `POST /api/v1/rooms/{roomNo}/nickname`
- `POST /api/v1/rooms/{roomNo}/heartbeat`
- `POST /api/v1/rooms/{roomNo}/transfers`
- `POST /api/v1/rooms/{roomNo}/transfers/{id}/action`
- `POST /api/v1/rooms/{roomNo}/messages`
- `GET /api/v1/rooms/{roomNo}/messages?cursor=...`
- `GET /api/v1/records`
- `GET /api/v1/records/{id}`
- `GET /ws/rooms/{roomNo}`


## 打包命令
```bash
cd /Users/yintian/my/goProject/go_mini_server
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o server ./cmd/api
  file server
```
