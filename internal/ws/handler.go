package ws

import (
	"log"
	"net/http"
	"strings"
	"time"

	"go_mini_server/internal/auth"
	"go_mini_server/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Handler struct {
	hub        *Hub
	jwtManager *auth.JWTManager
	svc        *service.Service
	upgrader   websocket.Upgrader
}

func NewHandler(hub *Hub, jwtManager *auth.JWTManager, svc *service.Service) *Handler {
	return &Handler{
		hub:        hub,
		jwtManager: jwtManager,
		svc:        svc,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}
}

func (handler *Handler) ServeRoom(c *gin.Context) {
	roomNo := strings.ToUpper(strings.TrimSpace(c.Param("roomNo")))
	if roomNo == "" {
		log.Printf("ws error: status=%d method=%s path=%s code=%s message=%s", http.StatusBadRequest, c.Request.Method, c.Request.URL.Path, "INVALID_ROOM_NO", "roomNo 不能为空")
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ROOM_NO", "message": "roomNo 不能为空"})
		return
	}

	token := extractToken(c.GetHeader("Authorization"))
	if token == "" {
		token = c.Query("token")
	}
	if token == "" {
		log.Printf("ws error: status=%d method=%s path=%s code=%s message=%s", http.StatusUnauthorized, c.Request.Method, c.Request.URL.Path, "UNAUTHORIZED", "缺少 token")
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "缺少 token"})
		return
	}

	userID, err := handler.jwtManager.Parse(token)
	if err != nil {
		log.Printf("ws error: status=%d method=%s path=%s code=%s message=%s", http.StatusUnauthorized, c.Request.Method, c.Request.URL.Path, "UNAUTHORIZED", "token 无效")
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "token 无效"})
		return
	}

	if err := handler.svc.CanAccessRoom(c.Request.Context(), userID, roomNo); err != nil {
		if appErr, ok := err.(*service.AppError); ok {
			log.Printf("ws error: status=%d method=%s path=%s code=%s message=%s", appErr.Status, c.Request.Method, c.Request.URL.Path, appErr.Code, appErr.Message)
			c.JSON(appErr.Status, appErr)
			return
		}
		log.Printf("ws error: status=%d method=%s path=%s err=%v", http.StatusInternalServerError, c.Request.Method, c.Request.URL.Path, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "服务异常"})
		return
	}

	conn, err := handler.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws error: method=%s path=%s upgrade=%v", c.Request.Method, c.Request.URL.Path, err)
		return
	}

	client := &Client{
		conn:   conn,
		send:   make(chan []byte, 128),
		userID: userID,
		roomNo: roomNo,
		hub:    handler.hub,
	}
	handler.hub.Register(client)

	go client.writePump()
	client.readPump()
}

func (client *Client) readPump() {
	defer func() {
		client.hub.Unregister(client)
		_ = client.conn.Close()
	}()

	client.conn.SetReadLimit(4096)
	_ = client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	client.conn.SetPongHandler(func(string) error {
		_ = client.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		if _, _, err := client.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (client *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			_ = client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func extractToken(authorization string) string {
	if authorization == "" {
		return ""
	}
	prefix := "Bearer "
	if strings.HasPrefix(authorization, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	}
	return strings.TrimSpace(authorization)
}
