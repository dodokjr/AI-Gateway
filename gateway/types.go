package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Message adalah satu pesan teks. Hanya teks yang didukung: content berupa
// string, atau array bagian bertipe "text". Gambar/video/audio ditolak.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *Message) UnmarshalJSON(b []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.Role = strings.ToLower(strings.TrimSpace(raw.Role))
	c := bytes.TrimSpace(raw.Content)
	switch {
	case len(c) == 0 || string(c) == "null":
		m.Content = ""
	case c[0] == '"':
		return json.Unmarshal(c, &m.Content)
	case c[0] == '[':
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(c, &parts); err != nil {
			return err
		}
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			if p.Type != "text" {
				return errors.New("hanya teks yang didukung; bagian bertipe '" + p.Type + "' ditolak (tanpa gambar/video)")
			}
			texts = append(texts, p.Text)
		}
		m.Content = strings.Join(texts, "\n")
	default:
		return errors.New("content harus berupa string atau array bagian teks")
	}
	return nil
}

// ChatRequest dipakai oleh /v1/chat dan /v1/chat/completions (format OpenAI).
type ChatRequest struct {
	Messages     []Message `json:"messages"`
	Prompt       string    `json:"prompt,omitempty"`   // alternatif singkat untuk 1 pesan user
	Provider     string    `json:"provider,omitempty"` // provider yang dicoba duluan
	Model        string    `json:"model,omitempty"`
	Temperature  *float64  `json:"temperature,omitempty"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	UseKnowledge *bool     `json:"use_knowledge,omitempty"` // default true
	Stream       bool      `json:"stream,omitempty"`
}

// Attempt mencatat satu percobaan yang gagal (untuk transparansi failover).
type Attempt struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
	Error    string `json:"error"`
}
