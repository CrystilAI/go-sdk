package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"

	"github.com/CrystilAI/go-sdk/internal"
)

// goModVersion returns the version go.mod requires for module, without its "v".
func goModVersion(t *testing.T, module string) string {
	t.Helper()
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == module {
			return strings.TrimPrefix(fields[1], "v")
		}
	}
	t.Fatalf("go.mod does not require %s", module)
	return ""
}

// TestPayloadNamesProviderVersion checks that payloads from each official SDK
// report its version, as Python does. Go test binaries record no dependencies,
// so the test supplies go.mod's.
func TestPayloadNamesProviderVersion(t *testing.T) {
	modules := map[crystil.Provider]string{
		crystil.ProviderOpenAI:    "github.com/openai/openai-go/v3",
		crystil.ProviderAnthropic: "github.com/anthropics/anthropic-sdk-go",
		crystil.ProviderGoogle:    "google.golang.org/genai",
	}
	want := map[crystil.Provider]string{}
	var deps []*debug.Module
	for provider, module := range modules {
		want[provider] = goModVersion(t, module)
		deps = append(deps, &debug.Module{Path: module, Version: "v" + want[provider]})
	}
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module { return deps }

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
	defer server.Close()

	ctx := context.Background()
	chat := openai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
	openaiClient := func(httpClient *http.Client) openai.Client {
		return openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
	}
	calls := []struct {
		name     string
		provider crystil.Provider
		call     func(httpClient *http.Client) error
	}{
		{"openai-go", crystil.ProviderOpenAI, func(httpClient *http.Client) error {
			ai := openaiClient(httpClient)
			_, err := ai.Chat.Completions.New(ctx, chat)
			return err
		}},
		{"openai-go stream", crystil.ProviderOpenAI, func(httpClient *http.Client) error {
			ai := openaiClient(httpClient)
			s := ai.Chat.Completions.NewStreaming(ctx, chat)
			for s.Next() {
			}
			return s.Err()
		}},
		{"anthropic-sdk-go", crystil.ProviderAnthropic, func(httpClient *http.Client) error {
			ai := anthropic.NewClient(
				anthropicoption.WithHTTPClient(httpClient),
				anthropicoption.WithBaseURL(server.URL),
				anthropicoption.WithAPIKey("test-key"),
			)
			_, err := ai.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     "claude-haiku-4-5",
				MaxTokens: 10,
				Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
			})
			return err
		}},
		{"genai", crystil.ProviderGoogle, func(httpClient *http.Client) error {
			gc, err := genai.NewClient(ctx, &genai.ClientConfig{
				Backend:     genai.BackendGeminiAPI,
				APIKey:      "test-key",
				HTTPClient:  httpClient,
				HTTPOptions: genai.HTTPOptions{BaseURL: server.URL},
			})
			if err != nil {
				return err
			}
			_, err = gc.Models.GenerateContent(ctx, "gemini-2.0-flash", genai.Text("hi"), nil)
			return err
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			c := newCollector(t)
			if err := tc.call(c.client.HTTPClient(tc.provider)); err != nil {
				t.Fatal(err)
			}
			if got := get(c.payload(t), "conversation.client.version"); got != want[tc.provider] {
				t.Errorf("conversation.client.version = %v, want %q", got, want[tc.provider])
			}
		})
	}
}
