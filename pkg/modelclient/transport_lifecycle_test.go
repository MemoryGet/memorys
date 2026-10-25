package modelclient

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The local web host creates an embedding client per request. Completing many
// such requests must not leave one live socket owned by each discarded client.
func TestRequestScopedModelClientsBoundConnections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		origins int
		budget  int
	}{{"same_endpoint", 1, 4}, {"multiple_endpoints", 12, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			connections := make(map[net.Conn]http.ConnState)
			servers := make([]*httptest.Server, tc.origins)
			for i := range servers {
				s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/tags" {
						t.Error("unexpected model request", r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{
						"name": "test:latest", "digest": strings.Repeat("a", 64),
					}}})
				}))
				s.Config.ConnState = func(c net.Conn, state http.ConnState) {
					mu.Lock()
					defer mu.Unlock()
					if state == http.StateClosed || state == http.StateHijacked {
						delete(connections, c)
					} else {
						connections[c] = state
					}
				}
				s.Start()
				t.Cleanup(s.Close)
				servers[i] = s
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for i := 0; i < 32; i++ {
				client, err := NewOllama(servers[i%len(servers)].URL, embeddingSpec())
				if err != nil {
					t.Fatal(err)
				}
				if err := client.Health(ctx); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				mu.Lock()
				live := len(connections)
				mu.Unlock()
				if live <= tc.budget {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("32 completed request-scoped model clients retained %d connections; budget %d", live, tc.budget)
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}
