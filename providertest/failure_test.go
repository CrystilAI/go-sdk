package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"
)

var chatParams = openai.ChatCompletionNewParams{
	Model:    "gpt-4o-mini",
	Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
}

var messageParams = anthropic.MessageNewParams{
	Model:     "claude-haiku-4-5",
	MaxTokens: 10,
	Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
}

// A call the provider SDK retries is recorded once, for the attempt that
// succeeded, not once for each attempt, as the Python SDK records it.
func TestRetriedCallRecordedOnce(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) <= 2 {
			w.Header().Set("Retry-After-Ms", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"Rate limit reached","type":"requests"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o-mini",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	c := newCollector(t)
	ai := openai.NewClient(option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
		option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
	if _, err := ai.Chat.Completions.New(context.Background(), chatParams); err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 3 {
		t.Fatalf("server got %d requests, want 2 failures and the retry that succeeded", n)
	}
	if got := len(c.payloads()); got != 1 {
		t.Errorf("collector got %d payloads, want 1, for the attempt that succeeded", got)
	}
}

// A call the provider answers with an error status is not recorded, as the
// Python SDK records none. Recorded, it would count as a succeeded call with no
// tokens, or be quarantined when the body is not JSON.
func TestFailedCallNotRecorded(t *testing.T) {
	calls := []struct {
		name        string
		status      int
		contentType string
		body        string
		provider    crystil.Provider
		call        func(ctx context.Context, httpClient *http.Client, url string) error
	}{
		{"openai-go", http.StatusBadRequest, "application/json", `{"error":{"message":"bad request"}}`, crystil.ProviderOpenAI,
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(url), option.WithAPIKey("test-key"))
				_, err := ai.Chat.Completions.New(ctx, chatParams)
				return err
			}},
		{"proxy error page", http.StatusBadGateway, "text/html", "<html><body>502 Bad Gateway</body></html>", crystil.ProviderOpenAI,
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(url), option.WithAPIKey("test-key"),
					option.WithMaxRetries(0))
				_, err := ai.Chat.Completions.New(ctx, chatParams)
				return err
			}},
		{"openai-go stream", http.StatusBadRequest, "application/json", `{"error":{"message":"bad request"}}`, crystil.ProviderOpenAI,
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(url), option.WithAPIKey("test-key"))
				s := ai.Chat.Completions.NewStreaming(ctx, chatParams)
				defer s.Close()
				for s.Next() {
				}
				return s.Err()
			}},
		{"anthropic-sdk-go stream", http.StatusBadRequest, "application/json",
			`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`, crystil.ProviderAnthropic,
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := anthropic.NewClient(anthropicoption.WithHTTPClient(httpClient), anthropicoption.WithBaseURL(url),
					anthropicoption.WithAPIKey("test-key"))
				s := ai.Messages.NewStreaming(ctx, messageParams)
				defer s.Close()
				for s.Next() {
				}
				return s.Err()
			}},
		{"genai stream", http.StatusBadRequest, "application/json",
			`{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`, crystil.ProviderGoogle,
			func(ctx context.Context, httpClient *http.Client, url string) error {
				gc, err := genai.NewClient(ctx, &genai.ClientConfig{
					Backend:     genai.BackendGeminiAPI,
					APIKey:      "test-key",
					HTTPClient:  httpClient,
					HTTPOptions: genai.HTTPOptions{BaseURL: url},
				})
				if err != nil {
					return err
				}
				for _, err := range gc.Models.GenerateContentStream(ctx, "gemini-2.0-flash", genai.Text("hi"), nil) {
					if err != nil {
						return err
					}
				}
				return nil
			}},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", call.contentType)
				w.WriteHeader(call.status)
				_, _ = io.WriteString(w, call.body)
			}))
			defer server.Close()

			c := newCollector(t)
			if err := call.call(context.Background(), c.client.HTTPClient(call.provider), server.URL); err == nil {
				t.Fatal("call succeeded, want the server's error")
			}
			if got := c.payloads(); len(got) != 0 {
				t.Errorf("collector got %d payloads, want none: %v", len(got), got)
			}
		})
	}
}

// A stream that fails partway is recorded as failed, as the Python SDK records
// it, though the provider SDK stops reading at the error event and the caller
// never closes the stream, as these SDKs' own streaming examples don't.
func TestStreamErrorRecordedFailed(t *testing.T) {
	calls := []struct {
		name     string
		provider crystil.Provider
		events   string
		wantExc  string
		call     func(ctx context.Context, httpClient *http.Client, url string) error
	}{
		{"openai-go", crystil.ProviderOpenAI,
			`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hi"}}]}` + "\n\n" +
				`data: {"error":{"message":"boom","type":"server_error"}}` + "\n\n", "boom",
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(url), option.WithAPIKey("test-key"))
				s := ai.Chat.Completions.NewStreaming(ctx, chatParams)
				for s.Next() {
				}
				return s.Err()
			}},
		{"anthropic-sdk-go", crystil.ProviderAnthropic,
			"event: message_start\n" + `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant",` +
				`"model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":5,"output_tokens":1}}}` + "\n\n" +
				"event: error\n" + `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n", "Overloaded",
			func(ctx context.Context, httpClient *http.Client, url string) error {
				ai := anthropic.NewClient(anthropicoption.WithHTTPClient(httpClient), anthropicoption.WithBaseURL(url),
					anthropicoption.WithAPIKey("test-key"))
				s := ai.Messages.NewStreaming(ctx, messageParams)
				for s.Next() {
				}
				return s.Err()
			}},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, call.events)
				w.(http.Flusher).Flush()
				<-release // the connection stays open, so the stream never reaches its end
			}))
			defer server.Close()
			defer close(release) // runs first, so the server can close

			c := newCollector(t)
			if err := call.call(context.Background(), c.client.HTTPClient(call.provider), server.URL); err == nil {
				t.Fatal("stream succeeded, want the error event's error")
			}
			p := c.payload(t)
			if status, exc := get(p, "meta.fnfg.status"), get(p, "meta.fnfg.exc"); status != "failed" || exc != call.wantExc {
				t.Errorf("status = %v, exc = %v; want failed, with exc %q", status, exc, call.wantExc)
			}
		})
	}
}

// A response whose body is cut short reaches the caller as it does without
// Crystil: the same error, after the same number of requests. Returned as a failed
// round trip instead, it made the provider SDK send the call, already billed,
// again. Some SDK versions retry a cut-short body themselves, so the count varies.
func TestResponseCutShortNotRetried(t *testing.T) {
	calls := []struct {
		name     string
		provider crystil.Provider
		call     func(ctx context.Context, httpClient *http.Client, url string) error
	}{
		{"openai-go", crystil.ProviderOpenAI, func(ctx context.Context, httpClient *http.Client, url string) error {
			ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(url), option.WithAPIKey("test-key"))
			_, err := ai.Chat.Completions.New(ctx, chatParams)
			return err
		}},
		{"anthropic-sdk-go", crystil.ProviderAnthropic, func(ctx context.Context, httpClient *http.Client, url string) error {
			ai := anthropic.NewClient(anthropicoption.WithHTTPClient(httpClient), anthropicoption.WithBaseURL(url),
				anthropicoption.WithAPIKey("test-key"))
			_, err := ai.Messages.New(ctx, messageParams)
			return err
		}},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.ReadAll(r.Body)
				conn, buf, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"id\":\"c1\",")
				_ = buf.Flush()
			}))
			defer server.Close()

			wantErr := call.call(context.Background(), &http.Client{}, server.URL)
			if wantErr == nil || !strings.Contains(wantErr.Error(), "unexpected EOF") {
				t.Fatalf("without Crystil, error = %v, want the cut-short body's unexpected EOF", wantErr)
			}
			wantRequests := requests.Swap(0)

			c := newCollector(t)
			err := call.call(context.Background(), c.client.HTTPClient(call.provider), server.URL)
			if err == nil || err.Error() != wantErr.Error() {
				t.Errorf("error = %v, want the one the call gets without Crystil: %v", err, wantErr)
			}
			if n := requests.Load(); n != wantRequests {
				t.Errorf("server got %d requests, want %d, as without Crystil: a retry bills the call again", n, wantRequests)
			}
			if got := c.payloads(); len(got) != 0 {
				t.Errorf("collector got %d payloads, want none for a failed call", len(got))
			}
		})
	}
}
