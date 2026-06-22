package game

type GameState string

const (
	StateLobby   GameState = "lobby"
	StatePicking GameState = "picking"
	StateDrawing GameState = "drawing"
	StateReveal  GameState = "reveal"
)

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
