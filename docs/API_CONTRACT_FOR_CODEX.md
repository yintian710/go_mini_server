# go_mini_server 接口对接文档（供 Codex 使用）

本文档基于当前代码实现整理（`internal/handler/api.go`、`internal/service/service.go`、`internal/ws/*`），用于让其他项目中的 Codex 快速对接本服务。

## 1. 基础约定

- Base URL：`http://<host>:<port>`（默认端口 `:8080`）
- API 前缀：`/api/v1`
- `roomNo` 规则：4~6 位纯数字字符串
- 鉴权方式：JWT
  - HTTP：`Authorization: Bearer <token>`（也兼容直接传 token 字符串）
  - WebSocket：优先 `Authorization`，否则可用 query `?token=<token>`
- 时间字段：Go `time.Time` 默认 JSON 格式（RFC3339）
- 成功响应：多数接口直接返回业务对象，状态码统一为 `200`
- 错误响应：

```json
{
  "code": "ERROR_CODE",
  "message": "错误说明"
}
```

常见错误状态码：`400 / 401 / 403 / 404 / 409 / 500`

## 2. 通用数据结构

### 2.1 User

```json
{
  "id": 1,
  "nickname": "玩家A",
  "avatarUrl": "https://...",
  "role": "normal",
  "activeAt": "2026-02-10T12:00:00Z"
}
```

- `role` 枚举：`normal | member | admin | super_admin`

### 2.2 LoginResult

```json
{
  "token": "<jwt>",
  "user": { "...": "User" }
}
```

### 2.3 RoomDetail

```json
{
  "room": {
    "id": 1,
    "roomNo": "102345",
    "ownerUserId": 100,
    "chatEnabled": false,
    "status": "active",
    "closedAt": null
  },
  "members": [
    {
      "userId": 100,
      "roomNickname": "庄家",
      "score": 0,
      "joinSeq": 1,
      "isOnline": true,
      "joinedAt": "2026-02-10T12:00:00Z",
      "lastSeenAt": "2026-02-10T12:00:00Z"
    }
  ]
}
```

- `room.status` 枚举：`active | settled`

### 2.4 Transfer

```json
{
  "id": 10,
  "fromUserId": 100,
  "toUserId": 101,
  "amount": 20,
  "status": "pending",
  "createdAt": "2026-02-10T12:00:00Z",
  "resolvedAt": null
}
```

- `status` 枚举：`pending | rolled_back | rejected`

### 2.5 Message

```json
{
  "id": 900,
  "roomId": 1,
  "senderUserId": 100,
  "type": "chat",
  "content": "hello",
  "transferId": 10,
  "createdAt": "2026-02-10T12:00:00Z"
}
```

- `type` 实际出现值：`system | chat | transfer`
- `senderUserId`、`transferId` 在值为 `0` 时会被省略（`omitempty`）

### 2.6 RecordSummary / RecordDetail

`GET /records` 返回项：

```json
{
  "recordId": 1,
  "roomNo": "102345",
  "finalScore": 30,
  "settledAt": "2026-02-10T12:00:00Z",
  "participant": 4
}
```

`GET /records/{id}` 返回：

```json
{
  "recordId": 1,
  "roomNo": "102345",
  "settledAt": "2026-02-10T12:00:00Z",
  "players": [
    {
      "userId": 100,
      "nicknameSnapshot": "玩家A",
      "finalScore": 30
    }
  ]
}
```

## 3. HTTP 接口清单

> 除特别标注外，均需 `Authorization`。

### 3.1 健康检查

#### GET `/healthz`（免鉴权）

响应：

```json
{ "status": "ok" }
```

---

### 3.2 认证与用户

#### POST `/api/v1/auth/register`（免鉴权）

请求：

```json
{
  "account": "user_1001",
  "nickname": "玩家1001"
}
```

响应：`LoginResult`

说明：
- `account` 不能为空，最多 64 字符
- `nickname` 可选，最多 32 字符；不传时服务端自动生成昵称
- 账号已存在时返回冲突错误

#### POST `/api/v1/auth/login`（免鉴权）

请求：

```json
{
  "account": "user_1001"
}
```

响应：`LoginResult`

说明：
- `account` 不能为空，最多 64 字符
- 仅允许已注册账号登录

#### POST `/api/v1/auth/wechat-login`（免鉴权）

请求：

```json
{ "code": "wx-login-code" }
```

响应：`LoginResult`

说明：
- `code` 不能为空
- 未配置微信参数时，服务端使用 mock openid 逻辑（便于本地联调）

#### GET `/api/v1/me`

响应：`User`

#### PATCH `/api/v1/me`

请求：

```json
{
  "nickname": "新昵称",
  "avatarUrl": "https://..."
}
```

响应：`User`

校验：
- `nickname` 必填
- `nickname` 最多 32 字符

---

### 3.3 激活码

#### POST `/api/v1/activation/redeem`

请求：

```json
{ "code": "ABCDEF123456" }
```

响应：`User`（用户角色会更新）

#### POST `/api/v1/activation/codes`

请求：

```json
{
  "targetRole": "member",
  "maxUses": 1,
  "expiresAt": "2026-03-01T00:00:00Z"
}
```

响应：

```json
{ "code": "GENERATED_CODE" }
```

校验/权限：
- 仅 `super_admin` 可创建
- `targetRole` 必须是合法角色
- `maxUses <= 0` 时服务端按 `1` 处理

---

### 3.4 房间

#### POST `/api/v1/rooms`

请求（可空 body）：

```json
{ "password": "123456" }
```

响应：`RoomDetail`

说明：
- 不传 `password` 或传空字符串表示无密码房间

#### POST `/api/v1/rooms/{roomNo}/join`

请求（可空 body）：

```json
{ "password": "123456" }
```

响应：`RoomDetail`

说明：
- `roomNo` 为纯数字字符串，长度 4~6 位

#### GET `/api/v1/rooms/{roomNo}`

响应：`RoomDetail`

#### POST `/api/v1/rooms/{roomNo}/leave`

响应：

```json
{ "ok": true }
```

#### POST `/api/v1/rooms/{roomNo}/chat-toggle`

请求：

```json
{ "enabled": true }
```

响应：

```json
{ "ok": true }
```

权限：仅房主

#### POST `/api/v1/rooms/{roomNo}/kick`

请求：

```json
{ "targetUserId": 101 }
```

响应：

```json
{ "ok": true }
```

权限：仅房主；且不能踢自己

#### POST `/api/v1/rooms/{roomNo}/nickname`

请求：

```json
{ "roomNickname": "我的房间昵称" }
```

响应：

```json
{ "ok": true }
```

校验：
- `roomNickname` 必填
- 最多 32 字符

#### POST `/api/v1/rooms/{roomNo}/heartbeat`

请求：空 body

响应：

```json
{ "ok": true }
```

---

### 3.5 积分转账

#### POST `/api/v1/rooms/{roomNo}/transfers`

请求：

```json
{
  "toUserId": 101,
  "amount": 20
}
```

响应：`Transfer`

规则：
- `amount` 不能为 `0`
- 若 `amount < 0`，不翻转 `fromUserId/toUserId`，仅按负数金额做反向记分
- 不能给自己转账

#### POST `/api/v1/rooms/{roomNo}/transfers/{id}/action`

请求：

```json
{ "action": "rollback" }
```

响应：`Transfer`

`action` 枚举：`rollback | reject`

权限：
- `rollback`：转出方或房主
- `reject`：接收方或房主

---

### 3.6 聊天消息

#### POST `/api/v1/rooms/{roomNo}/messages`

请求：

```json
{ "content": "这把我先出" }
```

响应：`Message`

校验/限流：
- `content` 必填
- 长度 <= 1000 字符
- 聊天开关关闭时返回 `CHAT_DISABLED`
- 发送限流：
  - 同用户两条聊天间隔至少 1 秒
  - 1 分钟最多 20 条
  - 最多连续发送 5 条

#### GET `/api/v1/rooms/{roomNo}/messages?cursor=<id>`

查询参数：
- `cursor` 可选，`int64`，需 `>= 0`
- 传入后查询 `id < cursor` 的历史消息

响应：

```json
{
  "items": [
    { "...": "Message" }
  ]
}
```

说明：
- 每次最多返回 50 条
- 按 `id DESC`（最新在前）

---

### 3.7 战绩

#### GET `/api/v1/records`

响应：

```json
{
  "items": [
    { "...": "RecordSummary" }
  ]
}
```

#### GET `/api/v1/records/{id}`

响应：`RecordDetail`

---

## 4. WebSocket 接口

### 4.1 连接

- URL：`GET /ws/rooms/{roomNo}`
- 鉴权：
  - Header：`Authorization: Bearer <token>`
  - 或 query：`?token=<token>`
- 前置校验：用户必须属于该房间（未离开）

### 4.2 推送消息格式

服务端广播统一结构：

```json
{
  "event": "member_joined",
  "payload": {}
}
```

当前事件与 payload：

1) `member_joined`

```json
{ "userId": 100 }
```

2) `member_left`

```json
{ "userId": 100, "reason": "主动离开" }
```

3) `score_changed`

```json
{ "...": "Transfer" }
```

4) `chat_message`

```json
{ "...": "Message" }
```

5) `room_settled`

```json
{ "roomNo": "102345" }
```

说明：
- 当前实现中，客户端发来的 WS 文本消息不会触发业务处理（仅维持连接）
- 服务端会定时发送 `Ping`

## 5. 错误码速查（实现中出现）

`ALREADY_REDEEMED`、`CHAT_DISABLED`、`CHAT_RATE_LIMIT`、`CODE_EXHAUSTED`、`CODE_EXPIRED`、`CODE_NOT_FOUND`、`FORBIDDEN`、`INVALID_ACTION`、`INVALID_AMOUNT`、`INVALID_CODE`、`INVALID_CONTENT`、`INVALID_CURSOR`、`INVALID_NICKNAME`、`INVALID_RECORD_ID`、`INVALID_REQUEST`、`INVALID_ROOM_NO`、`INVALID_TARGET`、`INVALID_TARGET_ROLE`、`INVALID_TRANSFER_ID`、`NOT_IN_ROOM`、`RECORD_NOT_FOUND`、`ROOM_MEMBER_NOT_FOUND`、`ROOM_NOT_FOUND`、`ROOM_PASSWORD_INVALID`、`ROOM_SETTLED`、`TARGET_NOT_IN_ROOM`、`TRANSFER_NOT_FOUND`、`TRANSFER_RESOLVED`、`UNAUTHORIZED`、`USER_NOT_FOUND`、`WECHAT_LOGIN_FAILED`。

---

## 6. 给对接方的建议（Codex 提示词可直接引用）

可在对接项目里给 Codex 传入以下约束：

1. 所有业务 API 都在 `/api/v1` 下；`/healthz` 和 `/ws/rooms/{roomNo}` 不在该前缀。
2. 除登录与健康检查外，HTTP 请求必须携带 `Authorization: Bearer <token>`。
3. 错误体统一解析 `{code, message}`，按 HTTP 状态做分支。
4. 列表接口返回 `{items: []}`，不是分页对象。
5. `roomNo` 必须是 4~6 位纯数字字符串。
6. WS 消息统一按 `{event, payload}` 分发处理。
