package crystil

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrystilAI/go-sdk/internal"
)

// newQueueClient returns a Client whose collector has no worker, so a test can
// read the payloads the transport sends from its queue.
func newQueueClient() *Client {
	cfg := &config{apiKey: "test-key", version: version}
	return &Client{
		cfg:       cfg,
		collector: &collector{cfg: cfg, queue: make(chan internal.Payload, 10), done: make(chan struct{})},
		txID:      "test-tx",
	}
}

// openai-go and go-openai leave content out of an assistant message that has
// only tool calls. The backend reads each message's content with no default, so
// the recorded query gives it one, null; the request is sent as the caller made it.
func TestQueryMessagesHaveContent(t *testing.T) {
	tests := []struct {
		name, path, body, list string
		wantContent            bool
	}{
		{"chat completions", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[` +
			`{"role":"user","content":"weather?"},` +
			`{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}],"role":"assistant"},` +
			`{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`, "messages", true},
		{"responses", "/v1/responses", `{"model":"gpt-4o-mini","input":[` +
			`{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{}"}]}`, "input", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				sent <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"c1"}`)
			}))
			defer server.Close()

			client := newQueueClient()
			resp, err := client.HTTPClient(ProviderOpenAI).Post(server.URL+tt.path, "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got := <-sent; string(got) != tt.body {
				t.Errorf("server got %s, want the body as sent: %s", got, tt.body)
			}

			var payload internal.Payload
			select {
			case payload = <-client.collector.queue:
			default:
				t.Fatal("no payload recorded")
			}
			list, _ := payload.Conversation.Query[tt.list].([]interface{})
			if len(list) == 0 {
				t.Fatalf("query has no %s: %v", tt.list, payload.Conversation.Query)
			}
			for i, item := range list {
				content, ok := item.(map[string]interface{})["content"]
				if ok != tt.wantContent {
					t.Errorf("%s[%d] has a content key: %v, want %v", tt.list, i, ok, tt.wantContent)
				}
				if i == 1 && content != nil {
					t.Errorf("%s[1].content = %v, want null for a message sent with none", tt.list, content)
				}
			}
		})
	}
}

// A 307 or 308 makes http.Client send the request again from the caller's
// GetBody. A caller who did not ask for usage still gets no usage chunk.
func TestRedirectedStreamHidesUsageChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old/chat/completions" {
			http.Redirect(w, r, "/v1/chat/completions", http.StatusPermanentRedirect)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		if bytes.Contains(body, []byte(`"include_usage":true`)) {
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"total_tokens\":6}}\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/old/chat/completions",
		strings.NewReader(`{"model":"gpt-4o-mini","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newQueueClient().HTTPClient(ProviderOpenAI).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.Request.URL.Path != "/v1/chat/completions" {
		t.Fatalf("request ended at %s, want the redirect followed", resp.Request.URL.Path)
	}
	if bytes.Contains(got, []byte(`"choices":[]`)) {
		t.Errorf("caller got the usage chunk after a redirect:\n%s", got)
	}
}

// RoundTrip leaves the caller's request as it was, as the http.RoundTripper
// contract requires, though it sends a changed body.
func TestRoundTripLeavesRequestAlone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	const body = `{"model":"gpt-4o-mini","stream":true,"messages":[]}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newQueueClient().HTTPClient(ProviderOpenAI).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if req.ContentLength != int64(len(body)) {
		t.Errorf("ContentLength = %d, want %d", req.ContentLength, len(body))
	}
	again, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(again); string(got) != body {
		t.Errorf("GetBody gives %s, want the caller's body: %s", got, body)
	}
}
