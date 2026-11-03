package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Dua KB (mensimulasikan dua instance serverless) berbagi satu "Upstash" palsu.
func TestUpstashSharedBetweenInstances(t *testing.T) {
	var mu sync.Mutex
	kv := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		var args []string
		_ = json.NewDecoder(r.Body).Decode(&args)
		mu.Lock()
		defer mu.Unlock()
		switch args[0] {
		case "SET":
			kv[args[1]] = args[2]
			_, _ = w.Write([]byte(`{"result":"OK"}`))
		case "GET":
			if v, ok := kv[args[1]]; ok {
				b, _ := json.Marshal(map[string]string{"result": v})
				_, _ = w.Write(b)
			} else {
				_, _ = w.Write([]byte(`{"result":null}`))
			}
		}
	}))
	t.Cleanup(srv.Close)
	st := UpstashStore{URL: srv.URL, Token: "tok", Key: "k"}
	a, b := NewKB(st), NewKB(st)
	if _, err := a.Add("Apa warna logo kami?", "Biru tua.", nil); err != nil {
		t.Fatal(err)
	}
	if got := b.Search("warna logo", 3); len(got) != 1 {
		t.Fatalf("instance lain harus melihat data: %+v", got)
	}
	if _, err := b.Add("Siapa CEO perusahaan?", "Bu Rina.", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.List()) != 2 {
		t.Fatal("instance a harus melihat tambahan dari b")
	}
	if a.StorageKind() != "upstash" {
		t.Fatal("kind salah")
	}
}
