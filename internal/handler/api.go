package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go_mini_server/internal/auth"
	"go_mini_server/internal/service"
	"go_mini_server/internal/ws"

	"github.com/gin-gonic/gin"
)

const contextUserID = "userID"

const (
	maxAvatarUploadBytes = 5 << 20
)

var allowedAvatarMime = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type API struct {
	svc              *service.Service
	jwtManager       *auth.JWTManager
	wsHandler        *ws.Handler
	avatarUploadDir  string
	avatarPublicBase string
}

func NewAPI(svc *service.Service, jwtManager *auth.JWTManager, wsHandler *ws.Handler, avatarUploadDir, avatarPublicBase string) *API {
	return &API{svc: svc, jwtManager: jwtManager, wsHandler: wsHandler, avatarUploadDir: avatarUploadDir, avatarPublicBase: strings.TrimRight(strings.TrimSpace(avatarPublicBase), "/")}
}

func (api *API) RegisterRoutes(router *gin.Engine) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.GET("/ws/rooms/:roomNo", api.wsHandler.ServeRoom)

	v1 := router.Group("/api/v1")
	{
		authGroup := v1.Group("/auth")
		authGroup.POST("/register", api.register)
		authGroup.POST("/login", api.login)
		authGroup.POST("/wechat-login", api.wechatLogin)

		protected := v1.Group("")
		protected.Use(api.authMiddleware())
		{
			protected.GET("/me", api.getMe)
			protected.PATCH("/me", api.updateMe)
			protected.POST("/uploads/avatar", api.uploadAvatar)

			protected.POST("/activation/redeem", api.redeemCode)
			protected.POST("/activation/codes", api.createActivationCode)

			protected.POST("/rooms", api.createRoom)
			protected.POST("/rooms/:roomNo/join", api.joinRoom)
			protected.GET("/rooms/:roomNo", api.getRoomDetail)
			protected.POST("/rooms/:roomNo/leave", api.leaveRoom)
			protected.POST("/rooms/:roomNo/chat-toggle", api.chatToggle)
			protected.POST("/rooms/:roomNo/kick", api.kickMember)
			protected.POST("/rooms/:roomNo/nickname", api.updateRoomNickname)
			protected.POST("/rooms/:roomNo/heartbeat", api.heartbeat)

			protected.POST("/rooms/:roomNo/transfers", api.createTransfer)
			protected.POST("/rooms/:roomNo/transfers/:id/action", api.transferAction)

			protected.POST("/rooms/:roomNo/messages", api.sendMessage)
			protected.GET("/rooms/:roomNo/messages", api.listMessages)

			protected.GET("/records", api.listRecords)
			protected.GET("/records/:id", api.getRecordDetail)
		}
	}
}

func (api *API) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c.GetHeader("Authorization"))
		if token == "" {
			log.Printf("api error: status=%d method=%s path=%s code=%s message=%s", http.StatusUnauthorized, c.Request.Method, c.Request.URL.Path, "UNAUTHORIZED", "缺少 token")
			c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "缺少 token"})
			c.Abort()
			return
		}

		userID, err := api.jwtManager.Parse(token)
		if err != nil {
			log.Printf("api error: status=%d method=%s path=%s code=%s message=%s", http.StatusUnauthorized, c.Request.Method, c.Request.URL.Path, "UNAUTHORIZED", "token 无效")
			c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "token 无效"})
			c.Abort()
			return
		}

		c.Set(contextUserID, userID)
		c.Next()
	}
}

func (api *API) wechatLogin(c *gin.Context) {
	var input service.WechatLoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}

	result, err := api.svc.WechatLogin(c.Request.Context(), input)
	if err != nil {
		writeErr(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (api *API) login(c *gin.Context) {
	var input service.LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}

	result, err := api.svc.Login(c.Request.Context(), input)
	if err != nil {
		writeErr(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (api *API) register(c *gin.Context) {
	var input service.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}

	result, err := api.svc.Register(c.Request.Context(), input)
	if err != nil {
		writeErr(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (api *API) getMe(c *gin.Context) {
	user, err := api.svc.GetMe(c.Request.Context(), mustUserID(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

func (api *API) updateMe(c *gin.Context) {
	var input service.UpdateMeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}

	user, err := api.svc.UpdateMe(c.Request.Context(), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

func (api *API) uploadAvatar(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeErr(c, service.NewBadRequest("INVALID_FILE", "缺少上传文件 file"))
		return
	}

	if fileHeader.Size <= 0 {
		writeErr(c, service.NewBadRequest("INVALID_FILE", "上传文件不能为空"))
		return
	}
	if fileHeader.Size > maxAvatarUploadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "FILE_TOO_LARGE", "message": "文件过大，最大 5MB"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		writeErr(c, err)
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	readN, readErr := io.ReadFull(file, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		writeErr(c, readErr)
		return
	}
	head = head[:readN]
	fileMime := strings.ToLower(strings.TrimSpace(http.DetectContentType(head)))
	ext, ok := allowedAvatarMime[fileMime]
	if !ok {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"code": "UNSUPPORTED_FILE_TYPE", "message": "仅支持 jpg/png/webp"})
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(c, err)
		return
	}

	userID := mustUserID(c)
	now := time.Now().UTC()
	key := fmt.Sprintf("avatars/%d/%s%s", userID, now.Format("20060102T150405.000000000Z"), ext)
	fullPath := filepath.Join(api.avatarUploadDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		writeErr(c, err)
		return
	}

	dst, err := os.Create(fullPath)
	if err != nil {
		writeErr(c, err)
		return
	}

	written, copyErr := io.Copy(dst, io.LimitReader(file, maxAvatarUploadBytes+1))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(fullPath)
		writeErr(c, copyErr)
		return
	}
	if closeErr != nil {
		_ = os.Remove(fullPath)
		writeErr(c, closeErr)
		return
	}
	if written > maxAvatarUploadBytes {
		_ = os.Remove(fullPath)
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "FILE_TOO_LARGE", "message": "文件过大，最大 5MB"})
		return
	}

	urlValue := api.buildAvatarPublicURL(c, key)
	c.JSON(http.StatusOK, gin.H{
		"url":  urlValue,
		"key":  key,
		"size": written,
		"mime": fileMime,
	})
}

func (api *API) buildAvatarPublicURL(c *gin.Context, key string) string {
	encodedPath := "uploads/" + strings.TrimLeft(url.PathEscape(filepath.ToSlash(key)), "/")
	encodedPath = strings.ReplaceAll(encodedPath, "%2F", "/")

	if api.avatarPublicBase != "" {
		return api.avatarPublicBase + "/" + encodedPath
	}

	host := strings.TrimSpace(c.Request.Host)
	if host == "" {
		host = "localhost:8080"
	}

	return "https://" + host + "/" + encodedPath
}

func (api *API) redeemCode(c *gin.Context) {
	var input service.RedeemCodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	user, err := api.svc.RedeemCode(c.Request.Context(), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

func (api *API) createActivationCode(c *gin.Context) {
	var input service.CreateActivationCodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	code, err := api.svc.CreateActivationCode(c.Request.Context(), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": code})
}

func (api *API) createRoom(c *gin.Context) {
	var input service.CreateRoomInput
	if err := c.ShouldBindJSON(&input); err != nil && !isBodyOptional(err) {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	detail, err := api.svc.CreateRoom(c.Request.Context(), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (api *API) joinRoom(c *gin.Context) {
	var input service.JoinRoomInput
	if err := c.ShouldBindJSON(&input); err != nil && !isBodyOptional(err) {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	detail, err := api.svc.JoinRoom(c.Request.Context(), mustUserID(c), c.Param("roomNo"), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (api *API) getRoomDetail(c *gin.Context) {
	detail, err := api.svc.GetRoomDetail(c.Request.Context(), c.Param("roomNo"))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (api *API) leaveRoom(c *gin.Context) {
	if err := api.svc.LeaveRoom(c.Request.Context(), c.Param("roomNo"), mustUserID(c), "主动离开"); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *API) chatToggle(c *gin.Context) {
	var input service.ChatToggleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	if err := api.svc.ToggleRoomChat(c.Request.Context(), c.Param("roomNo"), mustUserID(c), input); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *API) kickMember(c *gin.Context) {
	var input service.KickInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	if err := api.svc.KickMember(c.Request.Context(), c.Param("roomNo"), mustUserID(c), input); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *API) updateRoomNickname(c *gin.Context) {
	var input service.RoomNicknameInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	if err := api.svc.UpdateRoomNickname(c.Request.Context(), c.Param("roomNo"), mustUserID(c), input); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *API) heartbeat(c *gin.Context) {
	if err := api.svc.Heartbeat(c.Request.Context(), c.Param("roomNo"), mustUserID(c)); err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (api *API) createTransfer(c *gin.Context) {
	var input service.TransferInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	transfer, err := api.svc.CreateTransfer(c.Request.Context(), c.Param("roomNo"), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, transfer)
}

func (api *API) transferAction(c *gin.Context) {
	transferID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || transferID <= 0 {
		writeErr(c, service.NewBadRequest("INVALID_TRANSFER_ID", "转账 ID 错误"))
		return
	}

	var input service.TransferActionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}

	transfer, err := api.svc.TransferAction(c.Request.Context(), c.Param("roomNo"), mustUserID(c), transferID, input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, transfer)
}

func (api *API) sendMessage(c *gin.Context) {
	var input service.SendMessageInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeErr(c, service.NewBadRequest("INVALID_REQUEST", "请求参数错误"))
		return
	}
	message, err := api.svc.SendMessage(c.Request.Context(), c.Param("roomNo"), mustUserID(c), input)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, message)
}

func (api *API) listMessages(c *gin.Context) {
	cursor := int64(0)
	if v := strings.TrimSpace(c.Query("cursor")); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 0 {
			writeErr(c, service.NewBadRequest("INVALID_CURSOR", "cursor 错误"))
			return
		}
		cursor = parsed
	}
	messages, err := api.svc.ListMessages(c.Request.Context(), c.Param("roomNo"), cursor)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": messages})
}

func (api *API) listRecords(c *gin.Context) {
	items, err := api.svc.ListRecords(c.Request.Context(), mustUserID(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (api *API) getRecordDetail(c *gin.Context) {
	recordID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || recordID <= 0 {
		writeErr(c, service.NewBadRequest("INVALID_RECORD_ID", "record id 错误"))
		return
	}
	detail, err := api.svc.GetRecordDetail(c.Request.Context(), recordID)
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func mustUserID(c *gin.Context) int64 {
	v, ok := c.Get(contextUserID)
	if !ok {
		return 0
	}
	id, ok := v.(int64)
	if !ok {
		return 0
	}
	return id
}

func extractToken(authorization string) string {
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return ""
	}
	if strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	return authorization
}

func writeErr(c *gin.Context, err error) {
	if appErr, ok := err.(*service.AppError); ok {
		log.Printf("api error: status=%d method=%s path=%s code=%s message=%s", appErr.Status, c.Request.Method, c.Request.URL.Path, appErr.Code, appErr.Message)
		c.JSON(appErr.Status, appErr)
		return
	}
	log.Printf("api error: status=%d method=%s path=%s err=%v", http.StatusInternalServerError, c.Request.Method, c.Request.URL.Path, err)
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "服务异常"})
}

func isBodyOptional(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF")
}
