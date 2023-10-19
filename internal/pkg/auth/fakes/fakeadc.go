package fakes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// StartTokenValidationServer sets up a fake tokeninfo endpoint that responds with the given expiry for the given token.
func StartTokenValidationServer(t *testing.T, exp time.Time, validToken string) string {
	t.Helper()
	fakeTokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tokeninfo":
			token := r.URL.Query().Get("access_token")
			if token != validToken {
				t.Errorf("Expected query param access_token=%s, got access_token=%s", validToken, token)
			}
			resp := map[string]interface{}{
				"exp": strconv.FormatInt(exp.Unix(), 10),
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		case "/token":
			resp := map[string]interface{}{
				"access_token":  "adcToken",
				"expires_in":    3600,
				"refresh_token": "refresh",
				"scope":         "https://www.googleapis.com/auth/pubsub",
				"token_type":    "Bearer",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		default:
			t.Errorf("Unexpected request path, got '%s'", r.URL.Path)
		}
	}))
	t.Cleanup(fakeTokenServer.Close)
	return fakeTokenServer.URL
}
