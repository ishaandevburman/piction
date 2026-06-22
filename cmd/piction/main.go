package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/ishaandevburman/piction/internal/config"
	"github.com/ishaandevburman/piction/internal/game"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var roomManager *game.RoomManager

func main() {
	cfg := config.Load()

	roomManager = game.NewRoomManager(cfg)

	fs := http.FileServer(http.Dir("web/static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(path, "room/") {
			http.ServeFile(w, r, "web/index.html")
			return
		}
		http.Redirect(w, r, "/room/default", http.StatusFound)
	})

	http.HandleFunc("/ws", handleWS)

	log.Printf("piction starting on %s", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), nil); err != nil {
		log.Fatal(err)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("room")
	roomID = sanitizeRoomID(roomID)
	if roomID == "" {
		roomID = "default"
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	hub := roomManager.GetOrCreate(roomID)
	client := game.NewClient(hub, conn)
	hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

func sanitizeRoomID(id string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
}
