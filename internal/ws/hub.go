package ws

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type Event struct {
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

type Client struct {
	conn   *websocket.Conn
	send   chan []byte
	userID int64
	roomNo string

	hub *Hub
}

type Hub struct {
	mu       sync.RWMutex
	rooms    map[string]map[*Client]struct{}
	register chan *Client
	unreg    chan *Client
}

func NewHub() *Hub {
	hub := &Hub{
		rooms:    make(map[string]map[*Client]struct{}),
		register: make(chan *Client, 128),
		unreg:    make(chan *Client, 128),
	}
	go hub.run()
	return hub
}

func (hub *Hub) run() {
	for {
		select {
		case client := <-hub.register:
			hub.mu.Lock()
			if hub.rooms[client.roomNo] == nil {
				hub.rooms[client.roomNo] = make(map[*Client]struct{})
			}
			hub.rooms[client.roomNo][client] = struct{}{}
			hub.mu.Unlock()
		case client := <-hub.unreg:
			hub.removeClient(client)
		}
	}
}

func (hub *Hub) removeClient(client *Client) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	roomClients := hub.rooms[client.roomNo]
	if roomClients == nil {
		return
	}
	if _, exists := roomClients[client]; exists {
		delete(roomClients, client)
		close(client.send)
	}
	if len(roomClients) == 0 {
		delete(hub.rooms, client.roomNo)
	}
}

func (hub *Hub) Register(client *Client) {
	hub.register <- client
}

func (hub *Hub) Unregister(client *Client) {
	hub.unreg <- client
}

func (hub *Hub) Broadcast(roomNo, event string, payload any) {
	msg, err := json.Marshal(Event{Event: event, Payload: payload})
	if err != nil {
		log.Printf("marshal ws event failed: %v", err)
		return
	}

	hub.mu.RLock()
	clients := hub.rooms[roomNo]
	hub.mu.RUnlock()
	for client := range clients {
		select {
		case client.send <- msg:
		default:
			hub.Unregister(client)
		}
	}
}
