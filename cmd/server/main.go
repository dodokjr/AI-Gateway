// Server lokal: go run ./cmd/server
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ai-gateway/gateway"
)

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if i := strings.Index(v, " #"); i >= 0 { // komentar di akhir baris
			v = v[:i]
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			_ = os.Setenv(k, v)
		}
	}
}

func main() {
	cfgPath := flag.String("config", "", "path config.json (kosong = config.json jika ada, jika tidak bawaan)")
	listen := flag.String("listen", "127.0.0.1:8080", "alamat listen")
	flag.Parse()
	loadDotEnv(".env")

	path := *cfgPath
	if path == "" {
		if _, err := os.Stat("config.json"); err == nil {
			path = "config.json"
		}
	}
	cfg, total, err := gateway.LoadConfig(path)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Listen != "" && *listen == "127.0.0.1:8080" {
		*listen = cfg.Listen
	}
	if p := os.Getenv("PORT"); p != "" {
		*listen = ":" + p
	}
	srv, err := gateway.Build(cfg, total)
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("GATEWAY_KEY") == "" {
		log.Println("peringatan: GATEWAY_KEY kosong, endpoint tidak diamankan (jangan buka ke internet)")
	}

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Printf("AI gateway aktif di %s", *listen)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

