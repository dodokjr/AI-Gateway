// Fungsi serverless Vercel. Semua rute (/v1/*, /healthz) di-rewrite ke sini
// lewat vercel.json; path asli dibawa di query "__p".
package handler

import (
	"net/http"

	"ai-gateway/gateway"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Query().Get("__p"); p != "" {
		q := r.URL.Query()
		q.Del("__p")
		r.URL.Path = p
		r.URL.RawQuery = q.Encode()
	}
	gateway.Default().ServeHTTP(w, r)
}
