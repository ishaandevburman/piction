package main

import (
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type GameState string

const (
	StateLobby   GameState = "lobby"
	StatePicking GameState = "picking"
	StateDrawing GameState = "drawing"
	StateReveal  GameState = "reveal"
)

var wordBank = map[string][]string{
	"easy": {
		"pizza", "sun", "cat", "fish", "apple", "bird", "book", "cake", "door", "egg",
		"flag", "gift", "hat", "ice", "jam", "key", "leg", "moon", "nest", "owl",
		"pen", "rain", "star", "tree", "umbrella", "van", "watch", "box", "yarn", "zebra",
	},
	"medium": {
		"guitar", "camera", "bridge", "candle", "dragon", "eagle", "flute", "garden",
		"hammer", "island", "jigsaw", "kettle", "ladder", "mirror", "needle", "orange",
		"puzzle", "robot", "saddle", "tunnel", "vacuum", "waffle", "anchor", "barrel",
		"castle", "diamond", "engine", "feather", "glacier", "hammock",
	},
	"hard": {
		"chandelier", "microscope", "parachute", "skeleton", "thermometer", "accordion",
		"barometer", "calendula", "dodecahedron", "escalator", "flamingo", "gondola",
		"harmonica", "iguana", "jellyfish", "kaleidoscope", "labyrinth", "mannequin",
		"narwhal", "origami", "pantomime", "questionnaire", "rhinoceros", "saxophone",
		"tambourine", "ukulele", "ventriloquist", "windmill", "xylophone", "yacht",
	},
}

type WordOption struct {
	Word       string `json:"word"`
	Difficulty string `json:"difficulty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Stroke struct {
	ID        string  `json:"id"`
	Color     string  `json:"color"`
	BrushSize float64 `json:"brushSize"`
	Tool      string  `json:"tool"`
	Points    []Point `json:"points"`
}

type Player struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Score       int    `json:"score"`
	IsHost      bool   `json:"isHost"`
}

var difficultyDuration = map[string]time.Duration{
	"easy":   60 * time.Second,
	"medium": 45 * time.Second,
	"hard":   30 * time.Second,
}

type Hub struct {
	roomID          string
	roomManager     *RoomManager
	mu              sync.RWMutex
	clients         map[*Client]bool
	players         []Player
	state           GameState
	drawerID        string
	currentWord     string
	wordOptions     []WordOption
	wordTimer       *time.Timer
	activeStroke    *Stroke
	strokes         []Stroke
	correctGuessers []string
	drawingTimer    *time.Timer
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
	if !empty && !c.replaced {
		h.removePlayer(c.userID)
	}
	h.mu.Unlock()

	c.closeSend()

	if empty {
		h.stopTimer()
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
	if len(h.players) > 0 && !h.players[0].IsHost {
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
	wordOptions := make([]WordOption, len(h.wordOptions))
	copy(wordOptions, h.wordOptions)
	currentWord := h.currentWord
	strokes := make([]Stroke, len(h.strokes))
	copy(strokes, h.strokes)
	correctGuessers := make([]string, len(h.correctGuessers))
	copy(correctGuessers, h.correctGuessers)

	var difficulty string
	if state == StateDrawing || state == StateReveal {
		for _, wo := range wordOptions {
			if wo.Word == currentWord {
				difficulty = wo.Difficulty
				break
			}
		}
	}
	h.mu.Unlock()

	initPayload := map[string]any{
		"type":     "init",
		"players":  players,
		"state":    state,
		"drawerId": drawerID,
		"userId":   c.userID,
	}
	if state == StatePicking {
		initPayload["wordOptions"] = wordOptions
	}
	if state == StateDrawing || state == StateReveal {
		initPayload["strokes"] = strokes
	}
	if state == StateDrawing {
		initPayload["wordLen"] = len(currentWord)
		initPayload["difficulty"] = difficulty
		initPayload["duration"] = int(difficultyDuration[difficulty].Seconds())
		initPayload["correctGuessers"] = correctGuessers
		if c.userID == drawerID {
			initPayload["currentWord"] = currentWord
		}
	}
	if state == StateReveal {
		initPayload["word"] = currentWord
		initPayload["difficulty"] = difficulty
		initPayload["correctGuessers"] = correctGuessers
	}
	initMsg, _ := json.Marshal(initPayload)
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
	h.wordOptions = h.pickWordOptions()
	h.mu.Unlock()

	h.broadcastGameState()
	h.sendWordOptions()

	h.startTimer(15*time.Second, func() {
		h.autoPickWord()
	})
}

func (h *Hub) pickDrawer() string {
	if len(h.players) == 0 {
		return ""
	}
	return h.players[rand.Intn(len(h.players))].ID
}

func (h *Hub) pickWordOptions() []WordOption {
	opts := make([]WordOption, 0, 3)
	for _, diff := range []string{"easy", "medium", "hard"} {
		words := wordBank[diff]
		if len(words) == 0 {
			continue
		}
		opts = append(opts, WordOption{
			Word:       words[rand.Intn(len(words))],
			Difficulty: diff,
		})
	}
	return opts
}

func (h *Hub) HandlePickWord(c *Client, msg []byte) {
	h.mu.RLock()
	if h.state != StatePicking || c.userID != h.drawerID {
		h.mu.RUnlock()
		return
	}
	h.mu.RUnlock()

	var payload struct {
		Word string `json:"word"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	h.mu.Lock()
	valid := false
	var pickedDiff string
	for _, wo := range h.wordOptions {
		if wo.Word == payload.Word {
			valid = true
			pickedDiff = wo.Difficulty
			break
		}
	}
	if !valid {
		h.mu.Unlock()
		return
	}

	h.stopTimer()
	h.currentWord = payload.Word
	h.state = StateDrawing
	h.correctGuessers = nil
	h.strokes = nil
	h.wordOptions = nil
	duration := difficultyDuration[pickedDiff]
	h.drawingTimer = time.AfterFunc(duration, func() { h.handleTimeUp() })
	h.mu.Unlock()

	h.broadcastGameStateWithWord(pickedDiff)
	h.sendYourWord()
}

func (h *Hub) autoPickWord() {
	h.mu.Lock()
	if h.state != StatePicking || len(h.wordOptions) == 0 {
		h.mu.Unlock()
		return
	}
	picked := h.wordOptions[0]
	h.currentWord = picked.Word
	h.state = StateDrawing
	h.wordTimer = nil
	h.correctGuessers = nil
	h.strokes = nil
	h.wordOptions = nil
	duration := difficultyDuration[picked.Difficulty]
	h.drawingTimer = time.AfterFunc(duration, func() { h.handleTimeUp() })
	h.mu.Unlock()

	h.broadcastGameStateWithWord(picked.Difficulty)
	h.sendYourWord()
}

func (h *Hub) sendWordOptions() {
	h.mu.RLock()
	opts := make([]WordOption, len(h.wordOptions))
	copy(opts, h.wordOptions)
	drawer := h.clientByUserID(h.drawerID)
	h.mu.RUnlock()

	if drawer == nil {
		return
	}

	msg, _ := json.Marshal(map[string]any{
		"type":  "word-options",
		"words": opts,
	})
	select {
	case drawer.send <- msg:
	default:
	}
}

func (h *Hub) sendYourWord() {
	h.mu.RLock()
	drawer := h.clientByUserID(h.drawerID)
	word := h.currentWord
	h.mu.RUnlock()

	if drawer == nil {
		return
	}

	msg, _ := json.Marshal(map[string]any{
		"type": "your-word",
		"word": word,
	})
	select {
	case drawer.send <- msg:
	default:
	}
}

func (h *Hub) clientByUserID(userID string) *Client {
	for cl := range h.clients {
		if cl.userID == userID {
			return cl
		}
	}
	return nil
}

func (h *Hub) startTimer(d time.Duration, fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopTimerLocked()
	h.wordTimer = time.AfterFunc(d, fn)
}

func (h *Hub) stopTimer() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopTimerLocked()
}

func (h *Hub) stopTimerLocked() {
	if h.wordTimer != nil {
		h.wordTimer.Stop()
		h.wordTimer = nil
	}
	if h.drawingTimer != nil {
		h.drawingTimer.Stop()
		h.drawingTimer = nil
	}
}

func (h *Hub) handleTimeUp() {
	h.mu.Lock()
	if h.state != StateDrawing {
		h.mu.Unlock()
		return
	}
	h.state = StateReveal
	h.stopTimerLocked()
	h.mu.Unlock()

	h.broadcastReveal()
}

func (h *Hub) handleAllGuessed() {
	h.mu.Lock()
	if h.state != StateDrawing {
		h.mu.Unlock()
		return
	}
	h.state = StateReveal
	h.stopTimerLocked()
	h.mu.Unlock()

	h.broadcastReveal()
}

func (h *Hub) broadcastReveal() {
	h.mu.RLock()
	word := h.currentWord
	drawerID := h.drawerID
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(map[string]any{
		"type":     "game-state",
		"state":    StateReveal,
		"drawerId": drawerID,
		"word":     word,
	})

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
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

func (h *Hub) broadcastGameStateWithWord(difficulty string) {
	h.mu.RLock()
	state := h.state
	drawerID := h.drawerID
	wordLen := len(h.currentWord)
	duration := int(difficultyDuration[difficulty].Seconds())
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(map[string]any{
		"type":       "game-state",
		"state":      state,
		"drawerId":   drawerID,
		"wordLen":    wordLen,
		"difficulty": difficulty,
		"duration":   duration,
	})

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
}

func (h *Hub) BroadcastDraw(msg []byte, sender *Client) {
	h.mu.RLock()
	ok := h.state == StateDrawing && sender.userID == h.drawerID
	h.mu.RUnlock()
	if !ok {
		return
	}
	h.broadcastExcept(msg, sender)
}

func (h *Hub) BroadcastChat(msg []byte, sender *Client) {
	h.broadcastExcept(msg, sender)
}

func (h *Hub) Broadcast(msg []byte, sender *Client) {
	h.broadcastExcept(msg, sender)
}

func (h *Hub) HandleDraw(c *Client, msg []byte) {
	var payload struct {
		Action   string  `json:"action"`
		Stroke   *Stroke `json:"stroke,omitempty"`
		StrokeID string  `json:"strokeId,omitempty"`
		X        float64 `json:"x,omitempty"`
		Y        float64 `json:"y,omitempty"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	h.mu.Lock()
	if h.state != StateDrawing || c.userID != h.drawerID {
		h.mu.Unlock()
		return
	}

	switch payload.Action {
	case "begin":
		if payload.Stroke != nil {
			h.activeStroke = &Stroke{
				ID:        payload.Stroke.ID,
				Color:     payload.Stroke.Color,
				BrushSize: payload.Stroke.BrushSize,
				Tool:      payload.Stroke.Tool,
			}
		}
	case "point":
		if h.activeStroke != nil && payload.StrokeID == h.activeStroke.ID {
			h.activeStroke.Points = append(h.activeStroke.Points, Point{X: payload.X, Y: payload.Y})
		}
	case "end":
		if h.activeStroke != nil && payload.StrokeID == h.activeStroke.ID {
			stroke := *h.activeStroke
			stroke.Points = make([]Point, len(h.activeStroke.Points))
			copy(stroke.Points, h.activeStroke.Points)
			h.strokes = append(h.strokes, stroke)
			h.activeStroke = nil
		}
	case "clear":
		h.strokes = nil
		h.activeStroke = nil
	}
	h.mu.Unlock()

	h.broadcastExcept(msg, c)
}

func (h *Hub) HandleChat(c *Client, msg []byte) {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	h.mu.RLock()
	isGuess := h.state == StateDrawing && c.userID != h.drawerID
	word := h.currentWord
	alreadyGuessed := false
	for _, id := range h.correctGuessers {
		if id == c.userID {
			alreadyGuessed = true
			break
		}
	}
	h.mu.RUnlock()

	if !isGuess || alreadyGuessed {
		return
	}

	cleaned := strings.TrimSpace(strings.ToLower(payload.Message))
	target := strings.TrimSpace(strings.ToLower(word))
	isCorrect := cleaned == target

	if isCorrect {
		h.mu.Lock()
		alreadyGuessed = false
		for _, id := range h.correctGuessers {
			if id == c.userID {
				alreadyGuessed = true
				break
			}
		}
		if !alreadyGuessed {
			h.correctGuessers = append(h.correctGuessers, c.userID)
		}
		place := len(h.correctGuessers)
		allGuessed := place >= len(h.players)-1
		h.mu.Unlock()

		if alreadyGuessed {
			return
		}

		correctMsg, _ := json.Marshal(map[string]any{
			"type":        "correct-guess",
			"userId":      c.userID,
			"displayName": c.displayName,
			"place":       place,
		})
		h.broadcastAll(correctMsg)

		if allGuessed {
			h.handleAllGuessed()
		}
	} else {
		chatMsg, _ := json.Marshal(map[string]any{
			"type":    "chat",
			"user":    c.displayName,
			"userId":  c.userID,
			"message": payload.Message,
		})
		h.broadcastAll(chatMsg)
	}
}

func (h *Hub) broadcastAll(msg []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	for _, cl := range clients {
		select {
		case cl.send <- msg:
		default:
		}
	}
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
