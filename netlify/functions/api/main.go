// Fungsi Netlify. Membungkus http.Handler gateway ke format AWS Lambda
// (API Gateway proxy). Dependensi hanya aws-lambda-go.
package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"ai-gateway/gateway"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const prefix = "/.netlify/functions/api"

func handle(ctx context.Context, ev events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	path := strings.TrimPrefix(ev.Path, prefix)
	if path == "" {
		path = "/"
	}
	q := url.Values{}
	for k, vs := range ev.MultiValueQueryStringParameters {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	for k, v := range ev.QueryStringParameters {
		if _, ok := q[k]; !ok {
			q.Set(k, v)
		}
	}
	body := ev.Body
	if ev.IsBase64Encoded {
		if b, err := base64.StdEncoding.DecodeString(body); err == nil {
			body = string(b)
		}
	}
	u := path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, ev.HTTPMethod, u, strings.NewReader(body))
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: `{"error":"permintaan tidak valid"}`}, nil
	}
	for k, v := range ev.Headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	gateway.Default().ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	out := rec.Body.String()
	return events.APIGatewayProxyResponse{
		StatusCode: res.StatusCode,
		Headers:    map[string]string{"Content-Type": res.Header.Get("Content-Type")},
		Body:       out,
	}, nil
}

func main() { lambda.Start(handle) }
