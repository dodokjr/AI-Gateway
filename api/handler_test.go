package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRewrittenPath(t *testing.T) {
	_ = os.Setenv("GEMINI_API_KEYS", "dummy")
	_ = os.Setenv("GATEWAY_KEY", "s")
	req := httptest.NewRequest("GET", "/api?__p=/v1/status&x=1", nil)
	req.Header.Set("Authorization", "Bearer s")
	rec := httptest.NewRecorder()
	Handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("GET", "/api?__p=/healthz", nil)
	rec = httptest.NewRecorder()
	Handler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("healthz %d", rec.Code)
	}
}
