package game

const (
	points1st         = 10
	points2nd         = 7
	points3rd         = 5
	pointsLater       = 3
	drawerPoints      = 5
	revealTimeoutSecs = 6
)

func (h *Hub) awardScores() {
	switch h.cfg.ScoringMode {
	case "flat":
		h.awardScoresFlat()
	default:
		h.awardScoresStandard()
	}
}

func (h *Hub) awardScoresStandard() {
	for i, id := range h.correctGuessers {
		var pts int
		switch i {
		case 0:
			pts = points1st
		case 1:
			pts = points2nd
		case 2:
			pts = points3rd
		default:
			pts = pointsLater
		}
		for j := range h.players {
			if h.players[j].ID == id {
				h.players[j].Score += pts
				break
			}
		}
	}
	drawerPts := len(h.correctGuessers) * drawerPoints
	for j := range h.players {
		if h.players[j].ID == h.drawerID {
			h.players[j].Score += drawerPts
			break
		}
	}
}

func (h *Hub) awardScoresFlat() {
	if len(h.correctGuessers) == 0 {
		return
	}
	pts := points1st / len(h.correctGuessers)
	if pts < 1 {
		pts = 1
	}
	for _, id := range h.correctGuessers {
		for j := range h.players {
			if h.players[j].ID == id {
				h.players[j].Score += pts
				break
			}
		}
	}
	drawerPts := len(h.correctGuessers) * drawerPoints
	for j := range h.players {
		if h.players[j].ID == h.drawerID {
			h.players[j].Score += drawerPts
			break
		}
	}
}
