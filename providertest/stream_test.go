package providertest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// TestStreamedOpenAIChat checks that a streamed chat completion is recorded as
// the Python SDK records it, which is what the backend reads: every chunk merged,
// so choices keeps each chunk's delta, and usage comes from the last chunk.
func TestStreamedOpenAIChat(t *testing.T) {
	chunk := func(choices string) string {
		return `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-mini","choices":` + choices + `,"usage":null}`
	}
	chunks := []string{
		chunk(`[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]`),
		chunk(`[{"index":0,"delta":{"content":"Let me check."},"finish_reason":null}]`),
		chunk(`[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]`),
		chunk(`[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":\"Paris\"}"}}]},"finish_reason":null}]`),
		chunk(`[{"index":0,"delta":{},"finish_reason":"tool_calls"}]`),
	}
	usage := `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-mini","choices":[],` +
		`"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", c)
		}
		// OpenAI sends usage only when the request asks for it.
		if req.StreamOptions.IncludeUsage {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", usage)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	c := newCollector(t)
	ai := openai.NewClient(
		option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
		option.WithBaseURL(server.URL),
		option.WithAPIKey("test-key"),
	)
	s := ai.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("What is the weather in Paris?")},
	})
	for s.Next() {
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}

	response, ok := get(c.payload(t), "conversation.response").(map[string]any)
	if !ok {
		t.Fatal("conversation.response is not the merged chunks")
	}
	if total := get(response, "usage.total_tokens"); total != float64(29) {
		t.Errorf("usage = %v, want the last chunk's, with 29 total tokens", response["usage"])
	}
	choices, _ := response["choices"].([]any)
	if len(choices) != len(chunks) {
		t.Fatalf("choices has %d entries, want one per chunk: %d", len(choices), len(chunks))
	}
	var content strings.Builder
	var toolCalls int
	for i, choice := range choices {
		delta, _ := get(choice, "delta").(map[string]any)
		value, ok := delta["content"]
		if !ok {
			t.Errorf("choices[%d].delta has no content key; the backend reads it without a default", i)
		}
		if text, ok := value.(string); ok {
			content.WriteString(text)
		}
		calls, _ := delta["tool_calls"].([]any)
		toolCalls += len(calls)
	}
	if content.String() != "Let me check." || toolCalls != 2 {
		t.Errorf("deltas hold content %q and %d tool call parts, want %q and 2", content.String(), toolCalls, "Let me check.")
	}
	if last := get(choices[len(choices)-1], "finish_reason"); last != "tool_calls" {
		t.Errorf("last finish_reason = %v, want tool_calls", last)
	}
}

// A caller that did not ask for usage gets no usage chunk, though the call is
// recorded with the usage Crystil asked for: the chunk has empty choices, and
// code that reads Choices[0] without checking panics on it. A caller that asked
// for usage still gets it.
func TestStreamUsageChunkOnlyWhenAsked(t *testing.T) {
	const usage = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-mini","choices":[],` +
		`"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\","+
			"\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n")
		if req.StreamOptions.IncludeUsage {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", usage)
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	tests := []struct {
		name           string
		options        openai.ChatCompletionStreamOptionsParam
		wantUsageChunk bool
	}{
		{"not asked", openai.ChatCompletionStreamOptionsParam{}, false},
		{"turned off", openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(false)}, false},
		{"asked", openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCollector(t)
			ai := openai.NewClient(option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
				option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
			params := chatParams
			params.StreamOptions = tt.options
			s := ai.Chat.Completions.NewStreaming(context.Background(), params)
			var usageChunks int
			for s.Next() {
				if len(s.Current().Choices) == 0 {
					usageChunks++
				}
			}
			if err := s.Err(); err != nil {
				t.Fatal(err)
			}

			if got := usageChunks > 0; got != tt.wantUsageChunk {
				t.Errorf("caller got %d chunks with no choices, want a usage chunk: %v", usageChunks, tt.wantUsageChunk)
			}
			if total := get(c.payload(t), "conversation.response.usage.total_tokens"); total != float64(6) {
				t.Errorf("recorded total_tokens = %v, want the usage chunk's 6", total)
			}
		})
	}
}

// TestOpenAIResponsesStream checks that a streamed Responses API call records
// what the unstreamed call does: the Response object.
func TestOpenAIResponsesStream(t *testing.T) {
	texts := []struct {
		name string
		text string
	}{
		{"short", "Hello world"},
		// The final event holds the whole Response on one SSE line.
		{"longer than 64 KiB", strings.Repeat("a", 70<<10)},
	}
	for _, tc := range texts {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{
				"id": "resp_1", "object": "response", "created_at": 1, "status": "completed", "model": "gpt-4o-mini",
				"output": []any{map[string]any{
					"type": "message", "id": "msg_1", "status": "completed", "role": "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": tc.text, "annotations": []any{}}},
				}},
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
			}
			created := maps.Clone(response)
			created["status"] = "in_progress"
			created["output"] = []any{}
			delete(created, "usage")
			want, _ := json.Marshal(response)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte(`"stream":true`)) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(want)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []map[string]any{
					{"type": "response.created", "sequence_number": 0, "response": created},
					{"type": "response.output_text.delta", "sequence_number": 1, "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": tc.text},
					{"type": "response.completed", "sequence_number": 2, "response": response},
				} {
					data, _ := json.Marshal(event)
					_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
				}
			}))
			defer server.Close()

			for _, stream := range []bool{false, true} {
				ctx := context.Background()
				c := newCollector(t)
				ai := openai.NewClient(
					option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
					option.WithBaseURL(server.URL),
					option.WithAPIKey("test-key"),
				)
				params := responses.ResponseNewParams{
					Model: "gpt-4o-mini",
					Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("hi")},
				}
				if stream {
					s := ai.Responses.NewStreaming(ctx, params)
					for s.Next() {
					}
					if err := s.Err(); err != nil {
						t.Fatal(err)
					}
					if err := s.Close(); err != nil {
						t.Fatal(err)
					}
				} else if _, err := ai.Responses.New(ctx, params); err != nil {
					t.Fatal(err)
				}

				got, _ := json.Marshal(get(c.payload(t), "conversation.response"))
				if !bytes.Equal(got, want) {
					t.Errorf("stream=%v: recorded response is not the Response object: %.300s", stream, got)
				}
			}
		})
	}
}

// TestStreamSentOnce checks that a stream read to the end sends its payload
// without being closed, as openai-go and anthropic-sdk-go leave closing to the
// caller, and that closing it afterwards sends no second payload.
func TestStreamSentOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/v1/messages" {
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"usage":{"input_tokens":5,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_stop"}`,
			} {
				var e struct{ Type string }
				_ = json.Unmarshal([]byte(event), &e)
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, event)
			}
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\","+
			"\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	streams := []struct {
		name     string
		provider crystil.Provider
		read     func(ctx context.Context, httpClient *http.Client) (closeStream func() error, err error)
	}{
		{"openai", crystil.ProviderOpenAI, func(ctx context.Context, httpClient *http.Client) (func() error, error) {
			ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
			s := ai.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
				Model:    "gpt-4o-mini",
				Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
			})
			for s.Next() {
			}
			return s.Close, s.Err()
		}},
		{"anthropic", crystil.ProviderAnthropic, func(ctx context.Context, httpClient *http.Client) (func() error, error) {
			ai := anthropic.NewClient(
				anthropicoption.WithHTTPClient(httpClient),
				anthropicoption.WithBaseURL(server.URL),
				anthropicoption.WithAPIKey("test-key"),
			)
			s := ai.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
				Model:     "claude-haiku-4-5",
				MaxTokens: 10,
				Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
			})
			for s.Next() {
			}
			return s.Close, s.Err()
		}},
	}
	for _, tc := range streams {
		t.Run(tc.name, func(t *testing.T) {
			c := newCollector(t)
			closeStream, err := tc.read(context.Background(), c.client.HTTPClient(tc.provider))
			if err != nil {
				t.Fatal(err)
			}
			c.waitForPayload(t) // sent before the stream is closed

			for range 2 {
				if err := closeStream(); err != nil {
					t.Fatal(err)
				}
			}
			if got := c.payloads(); len(got) != 1 {
				t.Errorf("collector got %d payloads, want 1: closing the stream sent more", len(got))
			}
		})
	}
}
