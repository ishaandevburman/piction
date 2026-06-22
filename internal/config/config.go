package config

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerIP          string
	ServerPort        int
	ServerPassword    string
	WhiteList         []string
	EnforceWhitelist  bool
	LogIPs            bool
	MaxPlayers        int
	DifficultyPool    []string
	DrawingTimeEasy   time.Duration
	DrawingTimeMedium time.Duration
	DrawingTimeHard   time.Duration
	ScoringMode       string
	RoundsPerPlayer   int
	AutoAdvance       bool
	MOTD              string
	PlayerIdleTimeout time.Duration
	RateLimit         int
}

func Default() *Config {
	return &Config{
		ServerPort:        8080,
		DifficultyPool:    []string{"easy", "medium", "hard"},
		DrawingTimeEasy:   60 * time.Second,
		DrawingTimeMedium: 45 * time.Second,
		DrawingTimeHard:   30 * time.Second,
		ScoringMode:       "standard",
		AutoAdvance:       true,
	}
}

func Load() *Config {
	configPath := flag.String("config", "piction.properties", "path to config file")
	help := flag.Bool("help", false, "show usage")
	flag.BoolVar(help, "h", false, "show usage (shorthand)")

	var overrides []string
	flag.Func("D", "key=value  override a config property", func(s string) error {
		overrides = append(overrides, s)
		return nil
	})

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [-D key=value ...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Configuration is loaded from piction.properties (or --config path).\n")
		fmt.Fprintf(os.Stderr, "CLI -D flags override config file values.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	cfg := Default()

	if props := loadPropertiesFile(*configPath); props != nil {
		fmt.Printf("config: loaded %s\n", *configPath)
		applyProperties(cfg, props)
	} else if *configPath != "piction.properties" {
		fmt.Fprintf(os.Stderr, "config: file %s not found, using defaults + -D overrides\n", *configPath)
	}

	for _, kv := range overrides {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			setProperty(cfg, strings.TrimSpace(kv[:eq]), strings.TrimSpace(kv[eq+1:]))
		}
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	return cfg
}

func loadPropertiesFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if eq := strings.IndexByte(line, '='); eq >= 0 {
			key := strings.TrimSpace(line[:eq])
			value := strings.TrimSpace(line[eq+1:])
			if key != "" {
				props[key] = value
			}
		}
	}
	return props
}

func applyProperties(cfg *Config, props map[string]string) {
	for k, v := range props {
		setProperty(cfg, k, v)
	}
}

func setProperty(cfg *Config, key, value string) {
	switch key {
	case "server-ip":
		cfg.ServerIP = value
	case "server-port":
		if n, err := strconv.Atoi(value); err == nil {
			cfg.ServerPort = n
		}
	case "server-password":
		cfg.ServerPassword = value
	case "white-list":
		if value != "" {
			cfg.WhiteList = splitCSV(value)
		}
	case "enforce-whitelist":
		cfg.EnforceWhitelist = value == "true"
	case "log-ips":
		cfg.LogIPs = value == "true"
	case "max-players":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			cfg.MaxPlayers = n
		}
	case "difficulty-pool":
		if value != "" {
			cfg.DifficultyPool = splitCSV(value)
		}
	case "drawing-time-easy":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			cfg.DrawingTimeEasy = time.Duration(n) * time.Second
		}
	case "drawing-time-medium":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			cfg.DrawingTimeMedium = time.Duration(n) * time.Second
		}
	case "drawing-time-hard":
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			cfg.DrawingTimeHard = time.Duration(n) * time.Second
		}
	case "scoring-mode":
		cfg.ScoringMode = value
	case "rounds-per-player":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			cfg.RoundsPerPlayer = n
		}
	case "auto-advance":
		cfg.AutoAdvance = value == "true"
	case "motd":
		cfg.MOTD = value
	case "player-idle-timeout":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			cfg.PlayerIdleTimeout = time.Duration(n) * time.Second
		}
	case "rate-limit":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			cfg.RateLimit = n
		}
	}
}

func (c *Config) Validate() error {
	if c.ServerPort < 1 || c.ServerPort > 65535 {
		return fmt.Errorf("server-port must be 1-65535, got %d", c.ServerPort)
	}
	if len(c.DifficultyPool) == 0 {
		return fmt.Errorf("difficulty-pool must have at least one difficulty")
	}
	validDiffs := map[string]bool{"easy": true, "medium": true, "hard": true}
	for _, d := range c.DifficultyPool {
		if !validDiffs[d] {
			return fmt.Errorf("unknown difficulty %q in difficulty-pool", d)
		}
	}
	validModes := map[string]bool{"standard": true, "flat": true}
	if !validModes[c.ScoringMode] {
		fmt.Fprintf(os.Stderr, "warning: unknown scoring-mode %q, falling back to standard\n", c.ScoringMode)
		c.ScoringMode = "standard"
	}
	if c.MaxPlayers < 0 {
		return fmt.Errorf("max-players must be >= 0, got %d", c.MaxPlayers)
	}
	if c.RoundsPerPlayer < 0 {
		return fmt.Errorf("rounds-per-player must be >= 0, got %d", c.RoundsPerPlayer)
	}
	if c.RateLimit < 0 {
		return fmt.Errorf("rate-limit must be >= 0, got %d", c.RateLimit)
	}
	return nil
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.ServerIP, c.ServerPort)
}

func (c *Config) DrawingTime(difficulty string) time.Duration {
	switch difficulty {
	case "easy":
		return c.DrawingTimeEasy
	case "medium":
		return c.DrawingTimeMedium
	case "hard":
		return c.DrawingTimeHard
	default:
		return c.DrawingTimeMedium
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
