package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go_mini_server/internal/auth"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Broadcaster interface {
	Broadcast(roomNo, event string, payload any)
}

type Service struct {
	orm              *gorm.DB
	jwtManager       *auth.JWTManager
	wechatClient     *auth.WechatClient
	broadcaster      Broadcaster
	disconnectWindow time.Duration
}

type LoginResult struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func New(orm *gorm.DB, jwtManager *auth.JWTManager, wechatClient *auth.WechatClient, broadcaster Broadcaster, disconnectWindow time.Duration) *Service {
	return &Service{
		orm:              orm,
		jwtManager:       jwtManager,
		wechatClient:     wechatClient,
		broadcaster:      broadcaster,
		disconnectWindow: disconnectWindow,
	}
}

func (service *Service) WechatLogin(ctx context.Context, input WechatLoginInput) (LoginResult, error) {
	if strings.TrimSpace(input.Code) == "" {
		return LoginResult{}, NewBadRequest("INVALID_CODE", "code 不能为空")
	}

	openid, err := service.wechatClient.Code2Session(ctx, input.Code)
	if err != nil {
		return LoginResult{}, NewBadRequest("WECHAT_LOGIN_FAILED", "微信登录失败")
	}

	var user User
	err = service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model userModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("wx_openid = ?", openid).Take(&model).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}

			now := time.Now()
			model = userModel{
				WXOpenID:  openid,
				Nickname:  defaultNickname(openid),
				AvatarURL: "",
				Role:      RoleNormal,
				ActiveAt:  &now,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := tx.Create(&model).Error; err != nil {
				return err
			}
		}

		now := time.Now()
		if err := tx.Model(&userModel{}).Where("id = ?", model.ID).Updates(map[string]any{
			"active_at":  now,
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		model.ActiveAt = &now

		user = toUser(model)
		return nil
	})
	if err != nil {
		return LoginResult{}, mapError(err)
	}

	token, err := service.jwtManager.Generate(user.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate token: %w", err)
	}

	return LoginResult{Token: token, User: user}, nil
}

func (service *Service) GetMe(ctx context.Context, userID int64) (User, error) {
	var model userModel
	if err := service.orm.WithContext(ctx).Where("id = ?", userID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return User{}, NewNotFound("USER_NOT_FOUND", "用户不存在")
		}
		return User{}, err
	}
	return toUser(model), nil
}

func (service *Service) UpdateMe(ctx context.Context, userID int64, input UpdateMeInput) (User, error) {
	nickname := strings.TrimSpace(input.Nickname)
	if nickname == "" {
		return User{}, NewBadRequest("INVALID_NICKNAME", "昵称不能为空")
	}
	if len([]rune(nickname)) > 32 {
		return User{}, NewBadRequest("INVALID_NICKNAME", "昵称最多 32 个字符")
	}

	var model userModel
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", userID).Take(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("USER_NOT_FOUND", "用户不存在")
			}
			return err
		}

		model.Nickname = nickname
		model.AvatarURL = strings.TrimSpace(input.AvatarURL)
		model.UpdatedAt = time.Now()
		return tx.Save(&model).Error
	})
	if err != nil {
		return User{}, mapError(err)
	}

	return toUser(model), nil
}

func (service *Service) CreateActivationCode(ctx context.Context, userID int64, input CreateActivationCodeInput) (string, error) {
	if input.MaxUses <= 0 {
		input.MaxUses = 1
	}

	targetRole := UserRole(strings.TrimSpace(input.TargetRole))
	if !isValidRole(targetRole) {
		return "", NewBadRequest("INVALID_TARGET_ROLE", "targetRole 非法")
	}

	var actor userModel
	if err := service.orm.WithContext(ctx).Where("id = ?", userID).Take(&actor).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", NewUnauthorized("UNAUTHORIZED", "未登录")
		}
		return "", err
	}
	if actor.Role != RoleSuperAdmin {
		return "", NewForbidden("FORBIDDEN", "仅超级管理员可创建激活码")
	}

	for i := 0; i < 5; i++ {
		code, err := generateActivationCode()
		if err != nil {
			return "", err
		}

		now := time.Now()
		model := activationCodeModel{
			Code:       code,
			TargetRole: targetRole,
			MaxUses:    input.MaxUses,
			UsedCount:  0,
			ExpiresAt:  input.ExpiresAt,
			CreatedBy:  userID,
			CreatedAt:  now,
		}
		if err := service.orm.WithContext(ctx).Create(&model).Error; err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return "", err
		}
		return code, nil
	}

	return "", fmt.Errorf("failed to generate unique activation code")
}

func (service *Service) RedeemCode(ctx context.Context, userID int64, input RedeemCodeInput) (User, error) {
	code := strings.TrimSpace(input.Code)
	if code == "" {
		return User{}, NewBadRequest("INVALID_CODE", "激活码不能为空")
	}

	var user User
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var codeModel activationCodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).Take(&codeModel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("CODE_NOT_FOUND", "激活码不存在")
			}
			return err
		}

		if codeModel.UsedCount >= codeModel.MaxUses {
			return NewConflict("CODE_EXHAUSTED", "激活码已用完")
		}
		if codeModel.ExpiresAt != nil && codeModel.ExpiresAt.Before(time.Now()) {
			return NewConflict("CODE_EXPIRED", "激活码已过期")
		}

		redeem := activationRedeemModel{CodeID: codeModel.ID, UserID: userID, RedeemedAt: time.Now()}
		if err := tx.Create(&redeem).Error; err != nil {
			if isUniqueViolation(err) {
				return NewConflict("ALREADY_REDEEMED", "该用户已兑换过此激活码")
			}
			return err
		}

		if err := tx.Model(&activationCodeModel{}).Where("id = ?", codeModel.ID).Update("used_count", gorm.Expr("used_count + 1")).Error; err != nil {
			return err
		}

		var targetUser userModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&targetUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("USER_NOT_FOUND", "用户不存在")
			}
			return err
		}

		now := time.Now()
		targetUser.Role = codeModel.TargetRole
		targetUser.ActiveAt = &now
		targetUser.UpdatedAt = now
		if err := tx.Save(&targetUser).Error; err != nil {
			return err
		}

		user = toUser(targetUser)
		return nil
	})
	if err != nil {
		return User{}, mapError(err)
	}

	return user, nil
}

func (service *Service) CreateRoom(ctx context.Context, userID int64, input CreateRoomInput) (RoomDetail, error) {
	passwordHash := hashPassword(strings.TrimSpace(input.Password))

	var room roomModel
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user userModel
		if err := tx.Where("id = ?", userID).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("USER_NOT_FOUND", "用户不存在")
			}
			return err
		}

		for i := 0; i < 8; i++ {
			candidate, err := generateRoomNo()
			if err != nil {
				return err
			}

			var exists int64
			if err := tx.Model(&roomModel{}).Where("room_no = ?", candidate).Count(&exists).Error; err != nil {
				return err
			}
			if exists > 0 {
				continue
			}

			now := time.Now()
			room = roomModel{
				RoomNo:       candidate,
				PasswordHash: passwordHash,
				OwnerUserID:  userID,
				ChatEnabled:  false,
				Status:       "active",
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := tx.Create(&room).Error; err != nil {
				if isUniqueViolation(err) {
					continue
				}
				return err
			}
			break
		}
		if room.RoomNo == "" {
			return fmt.Errorf("generate room no failed")
		}

		now := time.Now()
		member := roomMemberModel{
			RoomID:       room.ID,
			UserID:       userID,
			RoomNickname: user.Nickname,
			Score:        0,
			JoinSeq:      1,
			JoinedAt:     now,
			LastSeenAt:   now,
			IsOnline:     true,
		}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}

		message := roomMessageModel{RoomID: room.ID, Type: "system", Content: "房间已创建", CreatedAt: now}
		return tx.Create(&message).Error
	})
	if err != nil {
		return RoomDetail{}, mapError(err)
	}

	return service.GetRoomDetail(ctx, room.RoomNo)
}

func (service *Service) JoinRoom(ctx context.Context, userID int64, roomNo string, input JoinRoomInput) (RoomDetail, error) {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	if roomNo == "" {
		return RoomDetail{}, NewBadRequest("INVALID_ROOM_NO", "roomNo 不能为空")
	}

	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}

		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}
		if !verifyPassword(room.PasswordHash, strings.TrimSpace(input.Password)) {
			return NewForbidden("ROOM_PASSWORD_INVALID", "密码错误")
		}

		var user userModel
		if err := tx.Where("id = ?", userID).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("USER_NOT_FOUND", "用户不存在")
			}
			return err
		}

		now := time.Now()
		var member roomMemberModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND user_id = ?", room.ID, userID).Take(&member).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			var maxJoinSeq int64
			if err := tx.Model(&roomMemberModel{}).Where("room_id = ?", room.ID).Select("COALESCE(MAX(join_seq),0)").Scan(&maxJoinSeq).Error; err != nil {
				return err
			}
			member = roomMemberModel{
				RoomID:       room.ID,
				UserID:       userID,
				RoomNickname: user.Nickname,
				Score:        0,
				JoinSeq:      maxJoinSeq + 1,
				JoinedAt:     now,
				LastSeenAt:   now,
				IsOnline:     true,
			}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		} else {
			member.RoomNickname = user.Nickname
			member.LeftAt = nil
			member.IsOnline = true
			member.LastSeenAt = now
			if err := tx.Save(&member).Error; err != nil {
				return err
			}
		}

		message := roomMessageModel{RoomID: room.ID, Type: "system", Content: fmt.Sprintf("%s 加入房间", user.Nickname), CreatedAt: now}
		return tx.Create(&message).Error
	})
	if err != nil {
		return RoomDetail{}, mapError(err)
	}

	detail, err := service.GetRoomDetail(ctx, roomNo)
	if err != nil {
		return RoomDetail{}, err
	}
	service.notify(roomNo, "member_joined", map[string]any{"userId": userID})
	return detail, nil
}

func (service *Service) GetRoomDetail(ctx context.Context, roomNo string) (RoomDetail, error) {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var room roomModel
	if err := service.orm.WithContext(ctx).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RoomDetail{}, NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return RoomDetail{}, err
	}

	var members []roomMemberModel
	if err := service.orm.WithContext(ctx).
		Where("room_id = ? AND left_at IS NULL", room.ID).
		Order("join_seq ASC").
		Find(&members).Error; err != nil {
		return RoomDetail{}, err
	}

	result := RoomDetail{Room: toRoom(room), Members: make([]RoomMember, 0, len(members))}
	for _, member := range members {
		result.Members = append(result.Members, toRoomMember(member))
	}
	return result, nil
}

func (service *Service) LeaveRoom(ctx context.Context, roomNo string, userID int64, reason string) error {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	if roomNo == "" {
		return NewBadRequest("INVALID_ROOM_NO", "roomNo 不能为空")
	}
	if reason == "" {
		reason = "主动离开"
	}

	settled := false
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}
		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}

		var member roomMemberModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND user_id = ? AND left_at IS NULL", room.ID, userID).Take(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_MEMBER_NOT_FOUND", "用户不在该房间")
			}
			return err
		}

		now := time.Now()
		member.IsOnline = false
		member.LeftAt = &now
		member.LastSeenAt = now
		if err := tx.Save(&member).Error; err != nil {
			return err
		}

		systemMessage := roomMessageModel{RoomID: room.ID, Type: "system", Content: fmt.Sprintf("用户 %d 离开房间（%s）", userID, reason), CreatedAt: now}
		if err := tx.Create(&systemMessage).Error; err != nil {
			return err
		}

		if room.OwnerUserID == userID {
			var nextOwner roomMemberModel
			err := tx.Where("room_id = ? AND left_at IS NULL AND is_online = true", room.ID).
				Order("join_seq ASC").
				Take(&nextOwner).Error
			if err == nil {
				room.OwnerUserID = nextOwner.UserID
				room.UpdatedAt = now
				if err := tx.Save(&room).Error; err != nil {
					return err
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var onlineCount int64
		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND left_at IS NULL AND is_online = true", room.ID).Count(&onlineCount).Error; err != nil {
			return err
		}
		if onlineCount == 0 {
			if err := service.settleRoomTx(tx, room.ID); err != nil {
				return err
			}
			settled = true
		}
		return nil
	})
	if err != nil {
		return mapError(err)
	}

	service.notify(roomNo, "member_left", map[string]any{"userId": userID, "reason": reason})
	if settled {
		service.notify(roomNo, "room_settled", map[string]any{"roomNo": roomNo})
	}
	return nil
}

func (service *Service) ToggleRoomChat(ctx context.Context, roomNo string, userID int64, input ChatToggleInput) error {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	return service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}
		if room.OwnerUserID != userID {
			return NewForbidden("FORBIDDEN", "仅房主可修改聊天开关")
		}
		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}

		now := time.Now()
		room.ChatEnabled = input.Enabled
		room.UpdatedAt = now
		if err := tx.Save(&room).Error; err != nil {
			return err
		}

		message := roomMessageModel{RoomID: room.ID, Type: "system", Content: fmt.Sprintf("聊天已%s", map[bool]string{true: "开启", false: "关闭"}[input.Enabled]), CreatedAt: now}
		return tx.Create(&message).Error
	})
}

func (service *Service) UpdateRoomNickname(ctx context.Context, roomNo string, userID int64, input RoomNicknameInput) error {
	nickname := strings.TrimSpace(input.RoomNickname)
	if nickname == "" {
		return NewBadRequest("INVALID_NICKNAME", "房间昵称不能为空")
	}
	if len([]rune(nickname)) > 32 {
		return NewBadRequest("INVALID_NICKNAME", "房间昵称最多 32 个字符")
	}

	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var room roomModel
	if err := service.orm.WithContext(ctx).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return err
	}

	result := service.orm.WithContext(ctx).Model(&roomMemberModel{}).
		Where("room_id = ? AND user_id = ? AND left_at IS NULL", room.ID, userID).
		Updates(map[string]any{"room_nickname": nickname, "last_seen_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return NewNotFound("ROOM_MEMBER_NOT_FOUND", "用户不在该房间")
	}
	return nil
}

func (service *Service) Heartbeat(ctx context.Context, roomNo string, userID int64) error {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var room roomModel
	if err := service.orm.WithContext(ctx).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return err
	}

	result := service.orm.WithContext(ctx).Model(&roomMemberModel{}).
		Where("room_id = ? AND user_id = ? AND left_at IS NULL", room.ID, userID).
		Updates(map[string]any{"last_seen_at": time.Now(), "is_online": true})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return NewNotFound("ROOM_MEMBER_NOT_FOUND", "用户不在该房间")
	}
	return nil
}

func (service *Service) KickMember(ctx context.Context, roomNo string, ownerUserID int64, input KickInput) error {
	if input.TargetUserID == ownerUserID {
		return NewBadRequest("INVALID_TARGET", "不能踢自己")
	}

	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	settled := false
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}
		if room.OwnerUserID != ownerUserID {
			return NewForbidden("FORBIDDEN", "仅房主可踢人")
		}
		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}

		var member roomMemberModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND user_id = ? AND left_at IS NULL", room.ID, input.TargetUserID).Take(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("TARGET_NOT_IN_ROOM", "目标用户不在房间")
			}
			return err
		}

		now := time.Now()
		member.IsOnline = false
		member.LeftAt = &now
		member.LastSeenAt = now
		if err := tx.Save(&member).Error; err != nil {
			return err
		}

		systemMessage := roomMessageModel{RoomID: room.ID, Type: "system", Content: fmt.Sprintf("用户 %d 被房主移出房间", input.TargetUserID), CreatedAt: now}
		if err := tx.Create(&systemMessage).Error; err != nil {
			return err
		}

		var onlineCount int64
		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND left_at IS NULL AND is_online = true", room.ID).Count(&onlineCount).Error; err != nil {
			return err
		}
		if onlineCount == 0 {
			if err := service.settleRoomTx(tx, room.ID); err != nil {
				return err
			}
			settled = true
		}
		return nil
	})
	if err != nil {
		return mapError(err)
	}

	service.notify(roomNo, "member_left", map[string]any{"userId": input.TargetUserID, "reason": "kicked"})
	if settled {
		service.notify(roomNo, "room_settled", map[string]any{"roomNo": roomNo})
	}
	return nil
}

func (service *Service) CreateTransfer(ctx context.Context, roomNo string, userID int64, input TransferInput) (Transfer, error) {
	if input.Amount == 0 {
		return Transfer{}, NewBadRequest("INVALID_AMOUNT", "amount 不能为 0")
	}

	fromUserID := userID
	toUserID := input.ToUserID
	amount := input.Amount
	if fromUserID == toUserID {
		return Transfer{}, NewBadRequest("INVALID_TARGET", "不能给自己转账")
	}

	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var transfer Transfer
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}
		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}

		if _, err := getActiveMemberForUpdate(tx, room.ID, fromUserID); err != nil {
			return err
		}
		if _, err := getActiveMemberForUpdate(tx, room.ID, toUserID); err != nil {
			return err
		}

		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND user_id = ?", room.ID, fromUserID).Update("score", gorm.Expr("score - ?", amount)).Error; err != nil {
			return err
		}
		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND user_id = ?", room.ID, toUserID).Update("score", gorm.Expr("score + ?", amount)).Error; err != nil {
			return err
		}

		now := time.Now()
		transferModel := scoreTransferModel{
			RoomID:     room.ID,
			FromUserID: fromUserID,
			ToUserID:   toUserID,
			Amount:     amount,
			Status:     "pending",
			CreatedAt:  now,
			ResolvedAt: nil,
		}
		if err := tx.Create(&transferModel).Error; err != nil {
			return err
		}

		senderID := userID
		message := roomMessageModel{
			RoomID:       room.ID,
			SenderUserID: &senderID,
			Type:         "transfer",
			Content:      fmt.Sprintf("%d -> %d %d 分", transferModel.FromUserID, transferModel.ToUserID, transferModel.Amount),
			TransferID:   &transferModel.ID,
			CreatedAt:    now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		transfer = toTransfer(transferModel)
		return nil
	})
	if err != nil {
		return Transfer{}, mapError(err)
	}

	service.notify(roomNo, "score_changed", transfer)
	return transfer, nil
}

func (service *Service) TransferAction(ctx context.Context, roomNo string, userID int64, transferID int64, input TransferActionInput) (Transfer, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "rollback" && action != "reject" {
		return Transfer{}, NewBadRequest("INVALID_ACTION", "action 仅支持 rollback/reject")
	}

	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var transfer Transfer
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}

		var transferModel scoreTransferModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND room_id = ?", transferID, room.ID).Take(&transferModel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("TRANSFER_NOT_FOUND", "转账记录不存在")
			}
			return err
		}
		if transferModel.Status != "pending" {
			return NewConflict("TRANSFER_RESOLVED", "转账已处理")
		}

		isOwner := userID == room.OwnerUserID
		if action == "rollback" && userID != transferModel.FromUserID && !isOwner {
			return NewForbidden("FORBIDDEN", "仅转出方或房主可撤回")
		}
		if action == "reject" && userID != transferModel.ToUserID && !isOwner {
			return NewForbidden("FORBIDDEN", "仅接收方或房主可拒绝")
		}

		if _, err := getMemberForUpdate(tx, room.ID, transferModel.FromUserID); err != nil {
			return err
		}
		if _, err := getMemberForUpdate(tx, room.ID, transferModel.ToUserID); err != nil {
			return err
		}

		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND user_id = ?", room.ID, transferModel.FromUserID).Update("score", gorm.Expr("score + ?", transferModel.Amount)).Error; err != nil {
			return err
		}
		if err := tx.Model(&roomMemberModel{}).Where("room_id = ? AND user_id = ?", room.ID, transferModel.ToUserID).Update("score", gorm.Expr("score - ?", transferModel.Amount)).Error; err != nil {
			return err
		}

		now := time.Now()
		transferModel.Status = map[string]string{"rollback": "rolled_back", "reject": "rejected"}[action]
		transferModel.ResolvedAt = &now
		transferModel.ResolvedByUserID = &userID
		if err := tx.Save(&transferModel).Error; err != nil {
			return err
		}

		message := roomMessageModel{RoomID: room.ID, Type: "system", Content: fmt.Sprintf("转账 %d 已%s", transferModel.ID, map[string]string{"rollback": "撤回", "reject": "拒绝"}[action]), TransferID: &transferModel.ID, CreatedAt: now}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		transfer = toTransfer(transferModel)
		return nil
	})
	if err != nil {
		return Transfer{}, mapError(err)
	}

	service.notify(roomNo, "score_changed", transfer)
	return transfer, nil
}

func (service *Service) SendMessage(ctx context.Context, roomNo string, userID int64, input SendMessageInput) (Message, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return Message{}, NewBadRequest("INVALID_CONTENT", "消息不能为空")
	}
	if len([]rune(content)) > 1000 {
		return Message{}, NewBadRequest("INVALID_CONTENT", "消息过长")
	}

	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var result Message
	err := service.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room roomModel
		if err := tx.Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
			}
			return err
		}
		if room.Status != "active" {
			return NewConflict("ROOM_SETTLED", "房间已结算")
		}
		if !room.ChatEnabled {
			return NewForbidden("CHAT_DISABLED", "聊天已关闭")
		}

		if _, err := getActiveMemberForUpdate(tx, room.ID, userID); err != nil {
			if appErr := new(AppError); errors.As(err, &appErr) && appErr.Code == "ROOM_MEMBER_NOT_FOUND" {
				return NewForbidden("NOT_IN_ROOM", "用户不在该房间")
			}
			return err
		}

		var last roomMessageModel
		err := tx.Where("room_id = ? AND sender_user_id = ? AND type = ?", room.ID, userID, "chat").Order("created_at DESC").Take(&last).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && time.Since(last.CreatedAt) < time.Second {
			return NewConflict("CHAT_RATE_LIMIT", "发送过快，请稍后")
		}

		var minuteCount int64
		if err := tx.Model(&roomMessageModel{}).
			Where("room_id = ? AND sender_user_id = ? AND type = ? AND created_at >= ?", room.ID, userID, "chat", time.Now().Add(-time.Minute)).
			Count(&minuteCount).Error; err != nil {
			return err
		}
		if minuteCount >= 20 {
			return NewConflict("CHAT_RATE_LIMIT", "1 分钟最多 20 条")
		}

		var latest []roomMessageModel
		if err := tx.Where("room_id = ? AND type = ?", room.ID, "chat").Order("created_at DESC").Limit(5).Find(&latest).Error; err != nil {
			return err
		}
		continuous := 0
		for _, message := range latest {
			if message.SenderUserID != nil && *message.SenderUserID == userID {
				continuous++
				continue
			}
			break
		}
		if continuous >= 5 {
			return NewConflict("CHAT_RATE_LIMIT", "最多连续发送 5 条")
		}

		sender := userID
		message := roomMessageModel{RoomID: room.ID, SenderUserID: &sender, Type: "chat", Content: content, CreatedAt: time.Now()}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		result = toMessage(message)
		return nil
	})
	if err != nil {
		return Message{}, mapError(err)
	}

	service.notify(roomNo, "chat_message", result)
	return result, nil
}

func (service *Service) ListMessages(ctx context.Context, roomNo string, cursor int64) ([]Message, error) {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var room roomModel
	if err := service.orm.WithContext(ctx).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return nil, err
	}

	query := service.orm.WithContext(ctx).Where("room_id = ?", room.ID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	var models []roomMessageModel
	if err := query.Order("id DESC").Limit(50).Find(&models).Error; err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(models))
	for _, model := range models {
		messages = append(messages, toMessage(model))
	}
	return messages, nil
}

func (service *Service) ListRecords(ctx context.Context, userID int64) ([]RecordSummary, error) {
	var playerRows []matchRecordPlayerModel
	if err := service.orm.WithContext(ctx).Where("user_id = ?", userID).Find(&playerRows).Error; err != nil {
		return nil, err
	}
	if len(playerRows) == 0 {
		return []RecordSummary{}, nil
	}

	recordIDs := make([]int64, 0, len(playerRows))
	finalScoreByRecord := make(map[int64]int64, len(playerRows))
	for _, row := range playerRows {
		recordIDs = append(recordIDs, row.RecordID)
		finalScoreByRecord[row.RecordID] = row.FinalScore
	}

	var records []matchRecordModel
	if err := service.orm.WithContext(ctx).Where("id IN ?", recordIDs).Order("settled_at DESC").Limit(100).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []RecordSummary{}, nil
	}

	roomIDs := make([]int64, 0, len(records))
	recordIDList := make([]int64, 0, len(records))
	for _, record := range records {
		roomIDs = append(roomIDs, record.RoomID)
		recordIDList = append(recordIDList, record.ID)
	}

	var rooms []roomModel
	if err := service.orm.WithContext(ctx).Where("id IN ?", roomIDs).Find(&rooms).Error; err != nil {
		return nil, err
	}
	roomNoByID := make(map[int64]string, len(rooms))
	for _, room := range rooms {
		roomNoByID[room.ID] = room.RoomNo
	}

	type participantCount struct {
		RecordID int64 `gorm:"column:record_id"`
		Count    int64 `gorm:"column:count"`
	}
	var counts []participantCount
	if err := service.orm.WithContext(ctx).
		Model(&matchRecordPlayerModel{}).
		Select("record_id, COUNT(*) AS count").
		Where("record_id IN ?", recordIDList).
		Group("record_id").
		Find(&counts).Error; err != nil {
		return nil, err
	}
	participantByRecord := make(map[int64]int64, len(counts))
	for _, count := range counts {
		participantByRecord[count.RecordID] = count.Count
	}

	items := make([]RecordSummary, 0, len(records))
	for _, record := range records {
		items = append(items, RecordSummary{
			RecordID:    record.ID,
			RoomNo:      roomNoByID[record.RoomID],
			FinalScore:  finalScoreByRecord[record.ID],
			SettledAt:   record.SettledAt,
			Participant: int(participantByRecord[record.ID]),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SettledAt.After(items[j].SettledAt)
	})
	return items, nil
}

func (service *Service) GetRecordDetail(ctx context.Context, recordID int64) (RecordDetail, error) {
	var record matchRecordModel
	if err := service.orm.WithContext(ctx).Where("id = ?", recordID).Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RecordDetail{}, NewNotFound("RECORD_NOT_FOUND", "战绩不存在")
		}
		return RecordDetail{}, err
	}

	var room roomModel
	if err := service.orm.WithContext(ctx).Where("id = ?", record.RoomID).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RecordDetail{}, NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return RecordDetail{}, err
	}

	var players []matchRecordPlayerModel
	if err := service.orm.WithContext(ctx).Where("record_id = ?", recordID).Order("final_score DESC, user_id ASC").Find(&players).Error; err != nil {
		return RecordDetail{}, err
	}

	detail := RecordDetail{RecordID: record.ID, RoomNo: room.RoomNo, SettledAt: record.SettledAt, Players: make([]RecordPlayer, 0, len(players))}
	for _, player := range players {
		detail.Players = append(detail.Players, RecordPlayer{UserID: player.UserID, NicknameSnapshot: player.NicknameSnapshot, FinalScore: player.FinalScore})
	}
	return detail, nil
}

func (service *Service) CanAccessRoom(ctx context.Context, userID int64, roomNo string) error {
	roomNo = strings.ToUpper(strings.TrimSpace(roomNo))
	var room roomModel
	if err := service.orm.WithContext(ctx).Where("room_no = ?", roomNo).Take(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return NewNotFound("ROOM_NOT_FOUND", "房间不存在")
		}
		return err
	}

	var count int64
	if err := service.orm.WithContext(ctx).Model(&roomMemberModel{}).
		Where("room_id = ? AND user_id = ? AND left_at IS NULL", room.ID, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return NewForbidden("NOT_IN_ROOM", "用户不在房间")
	}
	return nil
}

func (service *Service) CleanupDisconnected(ctx context.Context) error {
	var members []roomMemberModel
	if err := service.orm.WithContext(ctx).Where("is_online = true AND left_at IS NULL").Find(&members).Error; err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}

	roomIDSet := make(map[int64]struct{}, len(members))
	for _, member := range members {
		roomIDSet[member.RoomID] = struct{}{}
	}
	roomIDs := make([]int64, 0, len(roomIDSet))
	for roomID := range roomIDSet {
		roomIDs = append(roomIDs, roomID)
	}

	var rooms []roomModel
	if err := service.orm.WithContext(ctx).Where("id IN ? AND status = ?", roomIDs, "active").Find(&rooms).Error; err != nil {
		return err
	}
	roomNoByID := make(map[int64]string, len(rooms))
	for _, room := range rooms {
		roomNoByID[room.ID] = room.RoomNo
	}

	for _, member := range members {
		roomNo, ok := roomNoByID[member.RoomID]
		if !ok {
			continue
		}
		if time.Since(member.LastSeenAt) <= service.disconnectWindow {
			continue
		}
		if err := service.LeaveRoom(ctx, roomNo, member.UserID, "断连超时"); err != nil {
			if appErr := new(AppError); errors.As(err, &appErr) && appErr.Code == "ROOM_MEMBER_NOT_FOUND" {
				continue
			}
			return err
		}
	}

	return nil
}

func (service *Service) settleRoomTx(tx *gorm.DB, roomID int64) error {
	now := time.Now()
	result := tx.Model(&roomModel{}).
		Where("id = ? AND status = ?", roomID, "active").
		Updates(map[string]any{"status": "settled", "closed_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}

	record := matchRecordModel{RoomID: roomID, SettledAt: now, CreatedAt: now}
	if err := tx.Create(&record).Error; err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return err
	}

	var members []roomMemberModel
	if err := tx.Where("room_id = ?", roomID).Find(&members).Error; err != nil {
		return err
	}
	if len(members) > 0 {
		players := make([]matchRecordPlayerModel, 0, len(members))
		for _, member := range members {
			players = append(players, matchRecordPlayerModel{RecordID: record.ID, UserID: member.UserID, NicknameSnapshot: member.RoomNickname, FinalScore: member.Score})
		}
		if err := tx.Create(&players).Error; err != nil {
			return err
		}
	}

	message := roomMessageModel{RoomID: roomID, Type: "system", Content: "房间已自动结算", CreatedAt: now}
	return tx.Create(&message).Error
}

func (service *Service) notify(roomNo, event string, payload any) {
	if service.broadcaster != nil {
		service.broadcaster.Broadcast(strings.ToUpper(roomNo), event, payload)
	}
}

func isUniqueViolation(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return err
}

func isValidRole(role UserRole) bool {
	switch role {
	case RoleNormal, RoleMember, RoleAdmin, RoleSuperAdmin:
		return true
	default:
		return false
	}
}

func defaultNickname(openid string) string {
	if len(openid) >= 6 {
		return "用户" + strings.ToUpper(openid[len(openid)-6:])
	}
	return "新用户"
}

func getActiveMemberForUpdate(tx *gorm.DB, roomID, userID int64) (roomMemberModel, error) {
	var member roomMemberModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND user_id = ? AND left_at IS NULL", roomID, userID).Take(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return roomMemberModel{}, NewNotFound("ROOM_MEMBER_NOT_FOUND", "转账成员不存在")
		}
		return roomMemberModel{}, err
	}
	return member, nil
}

func getMemberForUpdate(tx *gorm.DB, roomID, userID int64) (roomMemberModel, error) {
	var member roomMemberModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND user_id = ?", roomID, userID).Take(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return roomMemberModel{}, NewNotFound("ROOM_MEMBER_NOT_FOUND", "转账成员不存在")
		}
		return roomMemberModel{}, err
	}
	return member, nil
}
