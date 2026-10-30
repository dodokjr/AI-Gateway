package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	gw     *Gateway
	kb     *KB
	apiKey string
	total  time.Duration // batas total satu permintaan chat (0 = tanpa batas)
}

func NewServer(gw *Gateway, kb *KB, apiKey string, total time.Duration) *Server {
	return &Server{gw: gw, kb: kb, apiKey: apiKey, total: total}
}

// Handler mengembalikan http.Handler lengkap (routing + autentikasi).
func (s *Server) Handler() http.Handler { return s.routes() }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON tidak valid: "+err.Error())
		return false
	}
	return true
}

func (s *Server) auth(next http.Handler) http.Handler {
	if s.apiKey == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if tok == "" {
				tok = r.Header.Get("X-API-Key")
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.apiKey)) != 1 {
				writeErr(w, http.StatusUnauthorized, "API key gateway salah/tidak ada")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/chat", func(w http.ResponseWriter, r *http.Request) { s.chat(w, r, false) })
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { s.chat(w, r, true) })
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"providers": s.gw.Status(), "storage": s.kb.StorageKind()})
	})
	mux.HandleFunc("POST /v1/train", s.train)
	mux.HandleFunc("GET /v1/train", s.listTrain)
	mux.HandleFunc("DELETE /v1/train/{id}", s.deleteTrain)
	mux.HandleFunc("POST /v1/feedback", s.feedback)
	mux.HandleFunc("GET /v1/persona", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"persona": s.kb.Persona()})
	})
	mux.HandleFunc("PUT /v1/persona", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Persona string `json:"persona"`
		}
		if !decode(w, r, &b) {
			return
		}
		if len(b.Persona) > 4000 {
			writeErr(w, 400, "persona terlalu panjang (maks 4000 karakter)")
			return
		}
		if err := s.kb.SetPersona(b.Persona); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"persona": s.kb.Persona()})
	})
	return s.auth(mux)
}

// splitMessages memisahkan system, memvalidasi role, dan mengambil pesan user terakhir.
func splitMessages(in []Message) (system string, msgs []Message, lastUser string, err error) {
	if len(in) == 0 {
		return "", nil, "", errors.New("messages kosong (atau isi 'prompt')")
	}
	if len(in) > 200 {
		return "", nil, "", errors.New("terlalu banyak pesan (maks 200)")
	}
	var sys []string
	for _, m := range in {
		switch m.Role {
		case "system", "developer":
			sys = append(sys, m.Content)
		case "user":
			msgs = append(msgs, m)
			lastUser = m.Content
		case "assistant", "model":
			msgs = append(msgs, Message{Role: "assistant", Content: m.Content})
		default:
			return "", nil, "", fmt.Errorf("role %q tidak dikenal (system|user|assistant)", m.Role)
		}
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "user" {
		return "", nil, "", errors.New("pesan terakhir harus dari user")
	}
	return strings.Join(sys, "\n\n"), msgs, lastUser, nil
}

// resolveModel: pada /v1/chat/completions, "model" boleh berisi nama provider
// ("gemini") atau "provider/model" ("groq/llama-3.3-70b-versatile"); "auto" = bebas.
func (s *Server) resolveModel(req *ChatRequest) {
	if req.Provider != "" || req.Model == "" || strings.EqualFold(req.Model, "auto") {
		return
	}
	for _, n := range s.gw.Names() {
		if strings.EqualFold(req.Model, n) {
			req.Provider, req.Model = n, ""
			return
		}
		if p, m, ok := strings.Cut(req.Model, "/"); ok && strings.EqualFold(p, n) {
			req.Provider, req.Model = n, m
			return
		}
	}
	req.Model = "" // nama model tak dikenal: pakai model bawaan tiap provider
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, compat bool) {
	var req ChatRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Stream {
		writeErr(w, 400, "stream belum didukung; set stream=false")
		return
	}
	if len(req.Messages) == 0 && strings.TrimSpace(req.Prompt) != "" {
		req.Messages = []Message{{Role: "user", Content: req.Prompt}}
	}
	if compat {
		s.resolveModel(&req)
	}
	userSys, msgs, lastUser, err := splitMessages(req.Messages)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var used []Example
	if req.UseKnowledge == nil || *req.UseKnowledge {
		used = s.kb.Search(lastUser, 3)
	}
	system := buildSystem(s.kb.Persona(), userSys, used)

	ctx := r.Context()
	if s.total > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.total)
		defer cancel()
	}
	res, err := s.gw.Chat(ctx, ChatOptions{
		Provider: req.Provider, Model: req.Model, Temperature: req.Temperature, MaxTokens: req.MaxTokens,
	}, system, msgs)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeErr(w, http.StatusGatewayTimeout, "batas waktu habis sebelum ada provider yang menjawab; naikkan timeout fungsi atau kurangi GATEWAY_TIMEOUT_SEC")
			return
		}
		var af *AllFailedError
		if errors.As(err, &af) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": af.Error(), "attempts": af.Attempts})
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if compat {
		writeJSON(w, 200, map[string]any{
			"id":      "chatcmpl-" + newID(),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   res.Provider + "/" + res.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]string{"role": "assistant", "content": res.Text},
				"finish_reason": "stop",
			}},
		})
		return
	}
	known := make([]map[string]string, 0, len(used))
	for _, e := range used {
		known = append(known, map[string]string{"id": e.ID, "question": e.Question})
	}
	writeJSON(w, 200, map[string]any{
		"text": res.Text, "provider": res.Provider, "model": res.Model,
		"failed_attempts": res.Failed, "knowledge_used": known,
	})
}

type trainItem struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Tags     []string `json:"tags"`
}

func (s *Server) train(w http.ResponseWriter, r *http.Request) {
	var b struct {
		trainItem
		Items []trainItem `json:"items"`
	}
	if !decode(w, r, &b) {
		return
	}
	items := b.Items
	if b.Question != "" || b.Answer != "" {
		items = append(items, b.trainItem)
	}
	if len(items) == 0 {
		writeErr(w, 400, "kirim {question, answer} atau {items:[...]}")
		return
	}
	if len(items) > 500 {
		writeErr(w, 400, "maks 500 item per permintaan")
		return
	}
	saved := make([]Example, 0, len(items))
	for i, it := range items {
		ex, err := s.kb.Add(it.Question, it.Answer, it.Tags)
		if err != nil {
			writeErr(w, 400, fmt.Sprintf("item %d: %v", i, err))
			return
		}
		saved = append(saved, ex)
	}
	writeJSON(w, 200, map[string]any{"saved": saved, "total": len(s.kb.List())})
}

func (s *Server) listTrain(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query().Get("q"); q != "" {
		writeJSON(w, 200, map[string]any{"items": s.kb.Search(q, 20)})
		return
	}
	items := s.kb.List()
	writeJSON(w, 200, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) deleteTrain(w http.ResponseWriter, r *http.Request) {
	ok, err := s.kb.Delete(r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "id tidak ditemukan")
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

// feedback: rating +1 pada jawaban bagus (disimpan sebagai contoh baru bila
// question+answer dikirim), -1 menurunkan skor contoh bernilai id tertentu.
func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID       string `json:"id"`
		Rating   int    `json:"rating"` // 1 atau -1
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Rating != 1 && b.Rating != -1 {
		writeErr(w, 400, "rating harus 1 atau -1")
		return
	}
	if b.ID != "" {
		ok, err := s.kb.Feedback(b.ID, b.Rating)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if !ok {
			writeErr(w, 404, "id tidak ditemukan")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "skor diperbarui"})
		return
	}
	if b.Rating == 1 && b.Question != "" && b.Answer != "" {
		ex, err := s.kb.Add(b.Question, b.Answer, nil)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		_, _ = s.kb.Feedback(ex.ID, 1)
		writeJSON(w, 200, map[string]any{"status": "disimpan sebagai pengetahuan baru", "id": ex.ID})
		return
	}
	writeErr(w, 400, "kirim id, atau question+answer dengan rating 1")
}
