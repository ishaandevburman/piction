package game

import (
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ishaandevburman/piction/internal/config"
)

type Hub struct {
	roomID      string
	cfg         *config.Config
	roomManager *RoomManager
	mu          sync.RWMutex
	clients     map[*Client]bool
	players     []Player
	state       GameState
	drawerID    string
	drawerIndex int
	round       int

	currentWord     string
	wordOptions     []WordOption
	wordTimer       *time.Timer
	activeStroke    *Stroke
	strokes         []Stroke
	correctGuessers []string
	drawingTimer    *time.Timer
	revealTimer     *time.Timer
	autoAdvance     bool
}

func NewHub(roomID string, cfg *config.Config) *Hub {
	return &Hub{
		roomID:      roomID,
		cfg:         cfg,
		clients:     make(map[*Client]bool),
		state:       StateLobby,
		autoAdvance: cfg.AutoAdvance,
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
		Password    string `json:"password,omitempty"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

	if h.cfg.ServerPassword != "" && payload.Password != h.cfg.ServerPassword {
		msg, _ := json.Marshal(map[string]string{"type": "error", "message": "wrong password"})
		select {
		case c.send <- msg:
		default:
		}
		return
	}

	c.userID = payload.UserID
	c.displayName = payload.DisplayName
	if c.displayName == "" {
		c.displayName = "Anonymous"
	}

	h.mu.Lock()
	if h.cfg.MaxPlayers > 0 && len(h.players) >= h.cfg.MaxPlayers {
		h.mu.Unlock()
		msg, _ := json.Marshal(map[string]string{"type": "error", "message": "room is full"})
		select {
		case c.send <- msg:
		default:
		}
		return
	}

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
	autoAdvance := h.autoAdvance

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
		"type":           "init",
		"players":        players,
		"state":          state,
		"drawerId":       drawerID,
		"userId":         c.userID,
		"autoAdvance":    autoAdvance,
		"motd":           h.cfg.MOTD,
		"maxPlayers":     h.cfg.MaxPlayers,
		"difficultyPool": h.cfg.DifficultyPool,
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
		initPayload["duration"] = int(h.cfg.DrawingTime(difficulty).Seconds())
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
	h.round = 1
	h.drawerIndex = rand.Intn(len(h.players))
	h.drawerID = h.players[h.drawerIndex].ID
	h.wordOptions = h.pickWordOptions()
	h.mu.Unlock()

	h.broadcastGameState()
	h.sendWordOptions()

	h.startTimer(15*time.Second, func() {
		h.autoPickWord()
	})
}

func (h *Hub) HandleNextRound(c *Client) {
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
	if h.state != StateReveal {
		h.mu.Unlock()
		return
	}
	if h.revealTimer != nil {
		h.revealTimer.Stop()
		h.revealTimer = nil
	}
	h.mu.Unlock()

	h.startNextRound()
}

func (h *Hub) startNextRound() {
	h.mu.Lock()
	if h.state != StateReveal {
		h.mu.Unlock()
		return
	}
	h.state = StatePicking

	if h.revealTimer != nil {
		h.revealTimer.Stop()
		h.revealTimer = nil
	}

	h.drawerIndex = (h.drawerIndex + 1) % len(h.players)
	h.drawerID = h.players[h.drawerIndex].ID
	h.currentWord = ""
	h.strokes = nil
	h.activeStroke = nil
	h.correctGuessers = nil
	h.round++

	if h.cfg.RoundsPerPlayer > 0 && h.round > h.cfg.RoundsPerPlayer*len(h.players) {
		h.state = StateLobby
		h.drawerID = ""
		h.wordOptions = nil
		h.mu.Unlock()
		h.broadcastGameState()
		h.broadcastPlayers()
		return
	}

	h.wordOptions = h.pickWordOptions()
	h.mu.Unlock()

	h.broadcastGameState()
	h.sendWordOptions()

	h.startTimer(15*time.Second, func() {
		h.autoPickWord()
	})
}

func (h *Hub) startRevealTimer() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.autoAdvance {
		return
	}
	if h.revealTimer != nil {
		h.revealTimer.Stop()
	}
	h.revealTimer = time.AfterFunc(time.Duration(revealTimeoutSecs)*time.Second, func() {
		h.startNextRound()
	})
}

func (h *Hub) HandleToggleAutoAdvance(c *Client, msg []byte) {
	var payload struct {
		AutoAdvance bool `json:"autoAdvance"`
	}
	if err := json.Unmarshal(msg, &payload); err != nil {
		return
	}

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
	h.autoAdvance = payload.AutoAdvance
	if !h.autoAdvance && h.revealTimer != nil {
		h.revealTimer.Stop()
		h.revealTimer = nil
	}
	if h.autoAdvance && h.state == StateReveal && h.revealTimer == nil {
		h.revealTimer = time.AfterFunc(time.Duration(revealTimeoutSecs)*time.Second, func() {
			h.startNextRound()
		})
	}
	h.mu.Unlock()

	h.broadcastReveal()
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

	h.stopTimerLocked()
	h.currentWord = payload.Word
	h.state = StateDrawing
	h.correctGuessers = nil
	h.strokes = nil
	h.wordOptions = nil
	duration := h.cfg.DrawingTime(pickedDiff)
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
	duration := h.cfg.DrawingTime(picked.Difficulty)
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
	if h.revealTimer != nil {
		h.revealTimer.Stop()
		h.revealTimer = nil
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
	h.awardScores()
	h.mu.Unlock()

	h.broadcastReveal()
	h.broadcastPlayers()
	h.startRevealTimer()
}

func (h *Hub) handleAllGuessed() {
	h.mu.Lock()
	if h.state != StateDrawing {
		h.mu.Unlock()
		return
	}
	h.state = StateReveal
	h.stopTimerLocked()
	h.awardScores()
	h.mu.Unlock()

	h.broadcastReveal()
	h.broadcastPlayers()
	h.startRevealTimer()
}

func (h *Hub) broadcastReveal() {
	h.mu.RLock()
	word := h.currentWord
	drawerID := h.drawerID
	players := make([]Player, len(h.players))
	copy(players, h.players)
	correctGuessers := make([]string, len(h.correctGuessers))
	copy(correctGuessers, h.correctGuessers)
	autoAdvance := h.autoAdvance
	clients := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		clients = append(clients, cl)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(map[string]any{
		"type":            "game-state",
		"state":           StateReveal,
		"drawerId":        drawerID,
		"word":            word,
		"players":         players,
		"correctGuessers": correctGuessers,
		"autoAdvance":     autoAdvance,
		"revealDuration":  revealTimeoutSecs,
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
	duration := int(h.cfg.DrawingTime(difficulty).Seconds())
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

// RoomManager manages all rooms.

type RoomManager struct {
	mu    sync.RWMutex
	rooms map[string]*Hub
	cfg   *config.Config
}

func NewRoomManager(cfg *config.Config) *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Hub),
		cfg:   cfg,
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

	hub = NewHub(roomID, rm.cfg)
	hub.roomManager = rm
	rm.rooms[roomID] = hub
	return hub
}

func (rm *RoomManager) removeRoom(roomID string) {
	rm.mu.Lock()
	delete(rm.rooms, roomID)
	rm.mu.Unlock()
}
