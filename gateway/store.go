package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store menyimpan basis pengetahuan. Di serverless (Vercel/Netlify) disk bersifat
// sementara, jadi gunakan Upstash Redis (REST) agar data training tidak hilang.
type Store interface {
	Load() ([]byte, error) // nil,nil jika belum ada data
	Save(b []byte) error
	Remote() bool // true = baca ulang tiap permintaan (instance bisa berbeda-beda)
	Kind() string
}

// ---------- File ----------

type FileStore struct {
	Path      string
	Ephemeral bool // true di serverless tanpa Upstash: data hilang saat instance daur ulang
}

func (f FileStore) Load() ([]byte, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (f FileStore) Save(b []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

func (f FileStore) Remote() bool { return false }
func (f FileStore) Kind() string {
	if f.Ephemeral {
		return "ephemeral"
	}
	return "file"
}

// ---------- Upstash Redis REST ----------

type UpstashStore struct {
	URL, Token, Key string
}

const maxRemoteBytes = 900_000 // batas request Upstash gratis ~1 MB

func (u UpstashStore) cmd(args ...string) (*string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out struct {
		Result *string `json:"result"`
		Error  string  `json:"error"`
	}
	err := doJSON(ctx, strings.TrimRight(u.URL, "/"), map[string]string{"Authorization": "Bearer " + u.Token}, args, &out)
	if err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New("upstash: " + out.Error)
	}
	return out.Result, nil
}

func (u UpstashStore) Load() ([]byte, error) {
	r, err := u.cmd("GET", u.Key)
	if err != nil || r == nil {
		return nil, err
	}
	return []byte(*r), nil
}

func (u UpstashStore) Save(b []byte) error {
	if len(b) > maxRemoteBytes {
		return fmt.Errorf("basis pengetahuan melebihi batas penyimpanan (%d KB)", maxRemoteBytes/1000)
	}
	_, err := u.cmd("SET", u.Key, string(b))
	return err
}

func (u UpstashStore) Remote() bool { return true }
func (u UpstashStore) Kind() string { return "upstash" }

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

func isServerless() bool {
	return os.Getenv("VERCEL") != "" || os.Getenv("NETLIFY") != "" || os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
}

// StoreFromEnv memilih penyimpanan: Upstash bila env tersedia (juga kompatibel
// dengan KV_REST_API_* dari integrasi Vercel), selain itu file.
func StoreFromEnv(kbPath string) Store {
	url := firstEnv("UPSTASH_REDIS_REST_URL", "KV_REST_API_URL")
	tok := firstEnv("UPSTASH_REDIS_REST_TOKEN", "KV_REST_API_TOKEN")
	if url != "" && tok != "" {
		return UpstashStore{URL: url, Token: tok, Key: "ai-gateway:knowledge"}
	}
	if isServerless() {
		return FileStore{Path: filepath.Join(os.TempDir(), "knowledge.json"), Ephemeral: true}
	}
	return FileStore{Path: kbPath}
}
