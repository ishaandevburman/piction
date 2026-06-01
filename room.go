package main

import (
	"encoding/json"
	"math/rand"
	"sync"
)

type GameState string

const (
	StateLobby  GameState = "lobby"
	StatePicking GameState = "picking"
	StateDrawing GameState = "drawing"
	StateReveal GameState = "reveal"
)

type Player struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Score       int    `json:"score"`
	IsHost      bool   `json:"isHost"`
}

type Hub struct {
	roomID      string
	roomManager *RoomManager
	mu          sync.RWMutex
	clients     map[*Client]bool
	players     []Player
	state       GameState
	drawerID    string
}

func NewHub(roomID string) *Hub {
	return &Hub{
		roomID:  roomID,
		clients: make(map[*Client]bool),
		state:   StateLobby,
	}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	empty := len(h.clients) == 0
	if !empty {
		h.removePlayer(c.userID)
	}
	h.mu.Unlock()

	c.closeSend()

	if empty {
		h.roomManager.removeRoom(h.roomID)
	}
}

func (h *Hub) addPlayer(userID, displayName string) {
	for i := range h.players {
		if h.players[i].ID == userID {
			h.players[i].DisplayName = displayName
			return
		}
	}
	isHost := len(h.players) == 0
	h.players = append(h.players, Player{
		ID:          userID,
		DisplayName: displayName,
		Score:       0,
		IsHost:      isHost,
	})
}

func (h *Hub) removePlayer(userID string) {
	for i := range h.players {
		if h.players[i].ID == userID {
			h.players = append(h.players[:i], h.players[i+1:]...)
			break
		}
	}
	if len(h.players) > 0 && h.players[0].IsHost == false {
		h.players[0].IsHost = true
	}
}

func (h *Hub) HandleJoin(c *Client, msg []byte) {
	var payload struct {
		UserID      string `json:"userId"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	c.userID = payload.UserID
	c.displayName = payload.DisplayName
	if c.displayName == "" {
		c.displayName = "Anonymous"
	}

	h.mu.Lock()
	for cl := range h.clients {
		if cl != c && cl.userID == payload.UserID {
			cl.replaced = true
			delete(h.clients, cl)
			cl.closeSend()
			cl.conn.Close()
		}
	}
	h.addPlayer(c.userID, c.displayName)
	players := make([]Player, len(h.players))
	copy(players, h.players)
	state := h.state
	drawerID := h.drawerID
	h.mu.Unlock()

	initMsg, _ := json.Marshal(map[string]any{
		"type":     "init",
		"players":  players,
		"state":    state,
		"drawerId": drawerID,
		"userId":   c.userID,
	})
	select {
	case c.send <- initMsg:
	default:
	}

	h.broadcastPlayers()
}

func (h *Hub) HandleSetName(c *Client, msg []byte) {
	var payload struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}
	c.displayName = payload.DisplayName

	h.mu.Lock()
	h.addPlayer(c.userID, c.displayName)
	h.mu.Unlock()

	h.broadcastPlayers()
}

func (h *Hub) HandleStartGame(c *Client) {
	h.mu.RLock()
	isHost := false
	for _, p := range h.players {
		if p.ID == c.userID && p.IsHost {
			isHost = true
			break
		}
	}
	h.mu.RUnlock()

	if !isHost {
		return
	}

	h.mu.Lock()
	if h.state != StateLobby {
		h.mu.Unlock()
		return
	}
	h.state = StatePicking
	h.drawerID = h.pickDrawer()
	h.mu.Unlock()

	h.broadcastGameState()
}

func (h *Hub) pickDrawer() string {
	if len(h.players) == 0 {
		return ""
	}
	return h.players[rand.Intn(len(h.players))].ID
}

func (h *Hub) broadcastPlayers() {
	h.mu.RLock()
	players := make([]Player, len(h.players))
	copy(players, h.players)
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(map[string]any{
		"type":    "players",
		"players": players,
	})

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
}

func (h *Hub) broadcastGameState() {
	h.mu.RLock()
	state := h.state
	drawerID := h.drawerID
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(map[string]any{
		"type":     "game-state",
		"state":    state,
		"drawerId": drawerID,
	})

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
}

func (h *Hub) BroadcastDraw(msg []byte, sender *Client) {
	h.broadcastExcept(msg, sender)
}

func (h *Hub) BroadcastChat(msg []byte, sender *Client) {
	h.broadcastExcept(msg, sender)
}

func (h *Hub) Broadcast(msg []byte, sender *Client) {
	h.broadcastExcept(msg, sender)
}

func (h *Hub) broadcastExcept(msg []byte, sender *Client) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		if cl != sender {
			clients = append(clients, cl)
		}
	}
	h.mu.RUnlock()

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
}

type RoomManager struct {
	mu    sync.RWMutex
	rooms map[string]*Hub
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Hub),
	}
}

func (rm *RoomManager) GetOrCreate(roomID string) *Hub {
	rm.mu.RLock()
	hub, ok := rm.rooms[roomID]
	rm.mu.RUnlock()
	if ok {
		return hub
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if hub, ok := rm.rooms[roomID]; ok {
		return hub
	}

	hub = NewHub(roomID)
	hub.roomManager = rm
	rm.rooms[roomID] = hub
	return hub
}

func (rm *RoomManager) removeRoom(roomID string) {
	rm.mu.Lock()
	delete(rm.rooms, roomID)
	rm.mu.Unlock()
}
