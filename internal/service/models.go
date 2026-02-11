package service

import "time"

type userModel struct {
	ID        int64      `gorm:"column:id;primaryKey"`
	WXOpenID  string     `gorm:"column:wx_openid"`
	Nickname  string     `gorm:"column:nickname"`
	AvatarURL string     `gorm:"column:avatar_url"`
	Role      UserRole   `gorm:"column:role"`
	ActiveAt  *time.Time `gorm:"column:active_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

func (userModel) TableName() string {
	return "users"
}

type activationCodeModel struct {
	ID         int64      `gorm:"column:id;primaryKey"`
	Code       string     `gorm:"column:code"`
	TargetRole UserRole   `gorm:"column:target_role"`
	MaxUses    int        `gorm:"column:max_uses"`
	UsedCount  int        `gorm:"column:used_count"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	CreatedBy  int64      `gorm:"column:created_by"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

func (activationCodeModel) TableName() string {
	return "activation_codes"
}

type activationRedeemModel struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	CodeID     int64     `gorm:"column:code_id"`
	UserID     int64     `gorm:"column:user_id"`
	RedeemedAt time.Time `gorm:"column:redeemed_at"`
}

func (activationRedeemModel) TableName() string {
	return "activation_redeems"
}

type roomModel struct {
	ID           int64      `gorm:"column:id;primaryKey"`
	RoomNo       string     `gorm:"column:room_no"`
	PasswordHash string     `gorm:"column:password_hash"`
	OwnerUserID  int64      `gorm:"column:owner_user_id"`
	ChatEnabled  bool       `gorm:"column:chat_enabled"`
	Status       string     `gorm:"column:status"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
	ClosedAt     *time.Time `gorm:"column:closed_at"`
}

func (roomModel) TableName() string {
	return "rooms"
}

type roomMemberModel struct {
	ID           int64      `gorm:"column:id;primaryKey"`
	RoomID       int64      `gorm:"column:room_id"`
	UserID       int64      `gorm:"column:user_id"`
	RoomNickname string     `gorm:"column:room_nickname"`
	Score        int64      `gorm:"column:score"`
	JoinSeq      int64      `gorm:"column:join_seq"`
	JoinedAt     time.Time  `gorm:"column:joined_at"`
	LastSeenAt   time.Time  `gorm:"column:last_seen_at"`
	LeftAt       *time.Time `gorm:"column:left_at"`
	IsOnline     bool       `gorm:"column:is_online"`
}

func (roomMemberModel) TableName() string {
	return "room_members"
}

type scoreTransferModel struct {
	ID               int64      `gorm:"column:id;primaryKey"`
	RoomID           int64      `gorm:"column:room_id"`
	FromUserID       int64      `gorm:"column:from_user_id"`
	ToUserID         int64      `gorm:"column:to_user_id"`
	Amount           int64      `gorm:"column:amount"`
	Status           string     `gorm:"column:status"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ResolvedAt       *time.Time `gorm:"column:resolved_at"`
	ResolvedByUserID *int64     `gorm:"column:resolved_by_user_id"`
}

func (scoreTransferModel) TableName() string {
	return "score_transfers"
}

type roomMessageModel struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	RoomID       int64     `gorm:"column:room_id"`
	SenderUserID *int64    `gorm:"column:sender_user_id"`
	Type         string    `gorm:"column:type"`
	Content      string    `gorm:"column:content"`
	TransferID   *int64    `gorm:"column:transfer_id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (roomMessageModel) TableName() string {
	return "room_messages"
}

type matchRecordModel struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	RoomID    int64     `gorm:"column:room_id"`
	SettledAt time.Time `gorm:"column:settled_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (matchRecordModel) TableName() string {
	return "match_records"
}

type matchRecordPlayerModel struct {
	ID               int64  `gorm:"column:id;primaryKey"`
	RecordID         int64  `gorm:"column:record_id"`
	UserID           int64  `gorm:"column:user_id"`
	NicknameSnapshot string `gorm:"column:nickname_snapshot"`
	FinalScore       int64  `gorm:"column:final_score"`
}

func (matchRecordPlayerModel) TableName() string {
	return "match_record_players"
}

func toUser(model userModel) User {
	activeAt := time.Time{}
	if model.ActiveAt != nil {
		activeAt = *model.ActiveAt
	}
	return User{
		ID:        model.ID,
		Nickname:  model.Nickname,
		AvatarURL: model.AvatarURL,
		Role:      model.Role,
		ActiveAt:  activeAt,
	}
}

func toRoom(model roomModel) Room {
	return Room{
		ID:          model.ID,
		RoomNo:      model.RoomNo,
		OwnerUserID: model.OwnerUserID,
		ChatEnabled: model.ChatEnabled,
		Status:      model.Status,
		ClosedAt:    model.ClosedAt,
	}
}

func toRoomMember(model roomMemberModel) RoomMember {
	return RoomMember{
		UserID:       model.UserID,
		RoomNickname: model.RoomNickname,
		Score:        model.Score,
		JoinSeq:      model.JoinSeq,
		IsOnline:     model.IsOnline,
		JoinedAt:     model.JoinedAt,
		LastSeenAt:   model.LastSeenAt,
	}
}

func toTransfer(model scoreTransferModel) Transfer {
	return Transfer{
		ID:         model.ID,
		FromUserID: model.FromUserID,
		ToUserID:   model.ToUserID,
		Amount:     model.Amount,
		Status:     model.Status,
		CreatedAt:  model.CreatedAt,
		ResolvedAt: model.ResolvedAt,
	}
}

func toMessage(model roomMessageModel) Message {
	senderID := int64(0)
	if model.SenderUserID != nil {
		senderID = *model.SenderUserID
	}
	transferID := int64(0)
	if model.TransferID != nil {
		transferID = *model.TransferID
	}
	return Message{
		ID:           model.ID,
		RoomID:       model.RoomID,
		SenderUserID: senderID,
		Type:         model.Type,
		Content:      model.Content,
		TransferID:   transferID,
		CreatedAt:    model.CreatedAt,
	}
}
