package providertest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newProviderServer returns a fake LLM server that answers OpenAI chat
// completions, plain and streamed, Anthropic messages and Gemini calls.
func newProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/messages":
			_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5",`+
				`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`)
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`)
		case strings.Contains(string(body), `"stream":true`):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\","+
				"\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
		default:
			_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o-mini",`+
				`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
