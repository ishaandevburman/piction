package game

import "math/rand"

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

func (h *Hub) pickWordOptions() []WordOption {
	pool := h.cfg.DifficultyPool
	opts := make([]WordOption, 0, 3)
	if len(pool) == 0 {
		return opts
	}
	for i := 0; i < 3; i++ {
		diff := pool[i%len(pool)]
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
