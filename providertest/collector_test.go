// Package providertest drives the real provider SDKs through the Crystil SDK
// against fake servers. It is its own module, so those SDKs stay out of the SDK's
// go.mod, and it checks each payload as a fake collector receives it.
package providertest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	crystil "github.com/CrystilAI/go-sdk"
)

// collector is a fake Crystil collector that keeps every payload the SDK sends,
// as the backend would receive it.
type collector struct {
	client  *crystil.Client
	mu      sync.Mutex
	got     []map[string]any
	arrived chan struct{}
	closed  sync.Once
}

// newCollector returns a fake collector whose client sends to it.
func newCollector(t *testing.T) *collector {
	t.Helper()
	t.Setenv("CRYSTIL_TEST_MODE", "") // in test mode the SDK sends nothing
	c := &collector{arrived: make(chan struct{}, 100)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("collector got a payload that isn't JSON: %v", err)
			return
		}
		c.mu.Lock()
		c.got = append(c.got, p)
		c.mu.Unlock()
		c.arrived <- struct{}{}
	}))
	t.Cleanup(server.Close)
	client, err := crystil.New(crystil.WithAPIKey("test-key"), crystil.WithCollectorURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	c.client = client
	t.Cleanup(c.closeClient) // runs before server.Close, so every payload arrives
	return c
}

func (c *collector) closeClient() {
	c.closed.Do(func() { c.client.Close() })
}

// payloads closes the client, which sends every payload it holds, and returns
// what the collector received, in the order the client sent it.
func (c *collector) payloads() []map[string]any {
	c.closeClient()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got
}

// payload returns the one payload the collector received.
func (c *collector) payload(t *testing.T) map[string]any {
	t.Helper()
	got := c.payloads()
	if len(got) != 1 {
		t.Fatalf("collector got %d payloads, want 1", len(got))
	}
	return got[0]
}

// waitForPayload waits for a payload to reach the collector while the client
// stays open.
func (c *collector) waitForPayload(t *testing.T) {
	t.Helper()
	select {
	case <-c.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("no payload reached the collector")
	}
}

// get returns the value at a dotted path in a decoded JSON object, or nil.
func get(v any, path string) any {
	for _, key := range strings.Split(path, ".") {
		object, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = object[key]
	}
	return v
}
