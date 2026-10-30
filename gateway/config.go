package gateway

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

//go:embed default_config.json
var defaultConfig []byte

// LoadConfig: path jika diberikan, lalu env CONFIG_JSON, lalu default_config.json (embed).
// Env GATEWAY_TIMEOUT_SEC (per percobaan) dan GATEWAY_TOTAL_TIMEOUT_SEC (total) menimpa nilai file.
func LoadConfig(path string) (Config, time.Duration, error) {
	raw := defaultConfig
	switch {
	case path != "":
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, 0, fmt.Errorf("tidak bisa membaca %s: %w", path, err)
		}
		raw = b
	case os.Getenv("CONFIG_JSON") != "":
		raw = []byte(os.Getenv("CONFIG_JSON"))
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, 0, fmt.Errorf("config tidak valid: %w", err)
	}
	if cfg.KBPath == "" {
		cfg.KBPath = "data/knowledge.json"
	}
	var total time.Duration
	if isServerless() {
		// Vercel: maxDuration 60 dtk (vercel.json). Netlify: default 10 dtk, bisa dinaikkan sampai 26 dtk.
		if os.Getenv("VERCEL") != "" {
			cfg.TimeoutSec, total = 25, 55*time.Second
		} else {
			cfg.TimeoutSec, total = 8, 24*time.Second
		}
	}
	if n, err := strconv.Atoi(os.Getenv("GATEWAY_TIMEOUT_SEC")); err == nil && n > 0 {
		cfg.TimeoutSec = n
	}
	if n, err := strconv.Atoi(os.Getenv("GATEWAY_TOTAL_TIMEOUT_SEC")); err == nil && n > 0 {
		total = time.Duration(n) * time.Second
	}
	return cfg, total, nil
}

// Build merakit Server dari config + env.
func Build(cfg Config, total time.Duration) (*Server, error) {
	gw, err := NewGateway(cfg)
	if err != nil {
		return nil, err
	}
	return NewServer(gw, NewKB(StoreFromEnv(cfg.KBPath)), os.Getenv("GATEWAY_KEY"), total), nil
}

var (
	once       sync.Once
	defHandler http.Handler
)

// Default dipakai fungsi serverless: dibangun sekali per instance dari env.
func Default() http.Handler {
	once.Do(func() {
		cfg, total, err := LoadConfig("")
		var srv *Server
		if err == nil {
			srv, err = Build(cfg, total)
		}
		if err != nil {
			msg := err.Error()
			defHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeErr(w, http.StatusInternalServerError, "konfigurasi gagal: "+msg)
			})
			return
		}
		defHandler = srv.Handler()
	})
	return defHandler
}
