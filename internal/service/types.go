package service

import "time"

type UserRole string

const (
	RoleNormal     UserRole = "normal"
	RoleMember     UserRole = "member"
	RoleAdmin      UserRole = "admin"
	RoleSuperAdmin UserRole = "super_admin"
)

type User struct {
	ID        int64     `json:"id"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatarUrl"`
	Role      UserRole  `json:"role"`
	ActiveAt  time.Time `json:"activeAt"`
}

type Room struct {
	ID          int64      `json:"id"`
	RoomNo      string     `json:"roomNo"`
	OwnerUserID int64      `json:"ownerUserId"`
	ChatEnabled bool       `json:"chatEnabled"`
	Status      string     `json:"status"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
}

type RoomMember struct {
	UserID       int64     `json:"userId"`
	RoomNickname string    `json:"roomNickname"`
	Score        int64     `json:"score"`
	JoinSeq      int64     `json:"joinSeq"`
	IsOnline     bool      `json:"isOnline"`
	JoinedAt     time.Time `json:"joinedAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
}

type RoomDetail struct {
	Room    Room         `json:"room"`
	Members []RoomMember `json:"members"`
}

type Transfer struct {
	ID         int64      `json:"id"`
	FromUserID int64      `json:"fromUserId"`
	ToUserID   int64      `json:"toUserId"`
	Amount     int64      `json:"amount"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

type Message struct {
	ID           int64     `json:"id"`
	RoomID       int64     `json:"roomId"`
	SenderUserID int64     `json:"senderUserId,omitempty"`
	Type         string    `json:"type"`
	Content      string    `json:"content"`
	TransferID   int64     `json:"transferId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type RecordSummary struct {
	RecordID    int64     `json:"recordId"`
	RoomNo      string    `json:"roomNo"`
	FinalScore  int64     `json:"finalScore"`
	SettledAt   time.Time `json:"settledAt"`
	Participant int       `json:"participant"`
}

type RecordDetail struct {
	RecordID  int64          `json:"recordId"`
	RoomNo    string         `json:"roomNo"`
	SettledAt time.Time      `json:"settledAt"`
	Players   []RecordPlayer `json:"players"`
}

type RecordPlayer struct {
	UserID           int64  `json:"userId"`
	NicknameSnapshot string `json:"nicknameSnapshot"`
	FinalScore       int64  `json:"finalScore"`
}

type CreateRoomInput struct {
	Password string `json:"password"`
}

type JoinRoomInput struct {
	Password string `json:"password"`
}

type UpdateMeInput struct {
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

type TransferInput struct {
	ToUserID int64 `json:"toUserId"`
	Amount   int64 `json:"amount"`
}

type TransferActionInput struct {
	Action string `json:"action"`
}

type SendMessageInput struct {
	Content string `json:"content"`
}

type RoomNicknameInput struct {
	RoomNickname string `json:"roomNickname"`
}

type KickInput struct {
	TargetUserID int64 `json:"targetUserId"`
}

type ChatToggleInput struct {
	Enabled bool `json:"enabled"`
}

type CreateActivationCodeInput struct {
	TargetRole string     `json:"targetRole"`
	MaxUses    int        `json:"maxUses"`
	ExpiresAt  *time.Time `json:"expiresAt"`
}

type RedeemCodeInput struct {
	Code string `json:"code"`
}

type WechatLoginInput struct {
	Code string `json:"code"`
}
