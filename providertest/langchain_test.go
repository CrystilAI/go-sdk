package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	lcopenai "github.com/tmc/langchaingo/llms/openai"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"

	"github.com/CrystilAI/go-sdk/internal"
)

// TestLangChainRequestsAreLabeled checks that a call langchaingo sends through
// HTTPClient is recorded as a LangChain call, with the title the other SDKs send
// for that LLM and langchaingo's version, while other requests stay unlabeled.
func TestLangChainRequestsAreLabeled(t *testing.T) {
	saved := internal.BuildDeps
	t.Cleanup(func() { internal.BuildDeps = saved })
	internal.BuildDeps = func() []*debug.Module {
		return []*debug.Module{{Path: "github.com/tmc/langchaingo", Version: "v0.1.13"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"4"}],"role":"model"},"finishReason":1}],`+
				`"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":1,"totalTokenCount":6},"modelVersion":"gemini-2.5-flash"}`)
			return
		}
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\","+
				"\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"4\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o-mini",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"4"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
	}))
	defer server.Close()

	// client is what a payload's conversation.client holds; "" fields are left out.
	type client struct{ provider, title, version string }
	langChain := client{"langchain", "chatopenai", "0.1.13"}
	calls := []struct {
		name     string
		provider crystil.Provider
		model    string
		call     func(ctx context.Context, httpClient *http.Client) error
		want     client
	}{
		{"langchaingo", crystil.ProviderOpenAI, "gpt-4o-mini", func(ctx context.Context, httpClient *http.Client) error {
			llm, err := lcopenai.New(lcopenai.WithModel("gpt-4o-mini"), lcopenai.WithToken("test-key"),
				lcopenai.WithBaseURL(server.URL), lcopenai.WithHTTPClient(httpClient))
			if err != nil {
				return err
			}
			_, err = llms.GenerateFromSinglePrompt(ctx, llm, "What is 2 + 2?")
			return err
		}, langChain},
		{"langchaingo google", crystil.ProviderGoogle, "models/gemini-2.5-flash", func(ctx context.Context, httpClient *http.Client) error {
			endpoint := func(o *googleai.Options) { o.ClientOptions = append(o.ClientOptions, option.WithEndpoint(server.URL)) }
			llm, err := googleai.New(ctx, googleai.WithAPIKey("test-key"), googleai.WithDefaultModel("gemini-2.5-flash"),
				googleai.WithRest(), googleai.WithHTTPClient(httpClient), endpoint)
			if err != nil {
				return err
			}
			_, err = llms.GenerateFromSinglePrompt(ctx, llm, "What is 2 + 2?")
			return err
		}, client{"langchain", "chatgooglegenai", "0.1.13"}},
		{"langchaingo stream", crystil.ProviderOpenAI, "gpt-4o-mini", func(ctx context.Context, httpClient *http.Client) error {
			llm, err := lcopenai.New(lcopenai.WithModel("gpt-4o-mini"), lcopenai.WithToken("test-key"),
				lcopenai.WithBaseURL(server.URL), lcopenai.WithHTTPClient(httpClient))
			if err != nil {
				return err
			}
			_, err = llms.GenerateFromSinglePrompt(ctx, llm, "What is 2 + 2?",
				llms.WithStreamingFunc(func(context.Context, []byte) error { return nil }))
			return err
		}, langChain},
		{"another library", crystil.ProviderOpenAI, "gpt-4o-mini", func(ctx context.Context, httpClient *http.Client) error {
			resp, err := httpClient.Post(server.URL+"/chat/completions", "application/json",
				strings.NewReader(`{"model":"gpt-4o-mini","messages":[]}`))
			if err == nil {
				_ = resp.Body.Close()
			}
			return err
		}, client{title: "openai"}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			c := newCollector(t)
			if err := tc.call(context.Background(), c.client.HTTPClient(tc.provider)); err != nil {
				t.Fatal(err)
			}
			p := c.payload(t)
			text := func(path string) string { s, _ := get(p, path).(string); return s }
			got := client{text("conversation.client.provider"), text("conversation.client.title"), text("conversation.client.version")}
			if got != tc.want {
				t.Errorf("conversation.client = %+v, want %+v", got, tc.want)
			}
			if model := get(p, "conversation.query.model"); model != tc.model {
				t.Errorf("query model = %v, want the request's, %s", model, tc.model)
			}
		})
	}
}

// TestLangChainCallbackCallKeepsItsProvider checks that a call to another provider
// made inside a langchaingo callback, such as a StreamingFunc, is not recorded as
// LangChain's: the backend would read it as a call to LangChain's provider.
func TestLangChainCallbackCallKeepsItsProvider(t *testing.T) {
	server := newProviderServer(t)
	c := newCollector(t)
	ai := anthropic.NewClient(anthropicoption.WithHTTPClient(c.client.HTTPClient(crystil.ProviderAnthropic)),
		anthropicoption.WithBaseURL(server.URL), anthropicoption.WithAPIKey("test-key"))
	llm, err := lcopenai.New(lcopenai.WithModel("gpt-4o-mini"), lcopenai.WithToken("test-key"),
		lcopenai.WithBaseURL(server.URL), lcopenai.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = llms.GenerateFromSinglePrompt(context.Background(), llm, "What is 2 + 2?",
		llms.WithStreamingFunc(func(ctx context.Context, _ []byte) error {
			_, err := ai.Messages.New(ctx, anthropic.MessageNewParams{
				Model:     "claude-haiku-4-5",
				MaxTokens: 10,
				Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
			})
			return err
		}))
	if err != nil {
		t.Fatal(err)
	}

	got := map[any]any{} // each payload's conversation.client title and provider
	for _, p := range c.payloads() {
		got[get(p, "conversation.client.title")] = get(p, "conversation.client.provider")
	}
	if want := map[any]any{"chatopenai": "langchain", "anthropic": nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("client titles and providers = %v, want %v", got, want)
	}
}

// TestLangChainGoogleStreamStoppedEarly checks that a stream langchaingo's googleai
// stops before its end, and never closes, is recorded once its context ends. A
// blocked candidate is one such stop; its prompt is still billed.
func TestLangChainGoogleStreamStoppedEarly(t *testing.T) {
	chunks := []string{
		`{"candidates":[{"content":{"parts":[{"text":"Once"}],"role":"model"}}]}`,
		`{"candidates":[{"content":{"parts":[{"text":" upon"}],"role":"model"},"finishReason":3}]}`,
	}
	// Each chunk goes out on its own, as Gemini streams them, so langchaingo stops
	// before the array ends. The server then waits for the client to hang up.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for i, chunk := range chunks {
			separator := ",\r\n"
			if i == 0 {
				separator = "["
			}
			_, _ = io.WriteString(w, separator+chunk)
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newCollector(t)
	endpoint := func(o *googleai.Options) { o.ClientOptions = append(o.ClientOptions, option.WithEndpoint(server.URL)) }
	llm, err := googleai.New(ctx, googleai.WithAPIKey("test-key"), googleai.WithDefaultModel("gemini-2.5-flash"),
		googleai.WithRest(), googleai.WithHTTPClient(c.client.HTTPClient(crystil.ProviderGoogle)), endpoint)
	if err != nil {
		t.Fatal(err)
	}

	_, err = llms.GenerateFromSinglePrompt(ctx, llm, "Tell me a story.",
		llms.WithStreamingFunc(func(context.Context, []byte) error { return nil }))
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("GenerateFromSinglePrompt() error = %v, want langchaingo's blocked error", err)
	}
	cancel()

	c.waitForPayload(t)
	if response, _ := get(c.payload(t), "conversation.response").([]any); len(response) != len(chunks) {
		t.Errorf("response = %v, want the chunks the server sent", response)
	}
}

// TestLangChainGoogleWithKeyClient gives langchaingo Crystil's copy of Google's
// API-key client, as examples/langchain-google does: the key must reach Gemini, since langchaingo
// adds none to a client it is given, and the call is recorded as LangChain's.
func TestLangChainGoogleWithKeyClient(t *testing.T) {
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key = r.Header.Get("X-Goog-Api-Key"); key == "" {
			key = r.URL.Query().Get("key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"4"}],"role":"model"},"finishReason":1}]}`)
	}))
	defer server.Close()

	ctx := context.Background()
	c := newCollector(t)
	httpClient, _, err := htransport.NewClient(ctx, option.WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := func(o *googleai.Options) { o.ClientOptions = append(o.ClientOptions, option.WithEndpoint(server.URL)) }
	llm, err := googleai.New(ctx, googleai.WithAPIKey("test-key"), googleai.WithDefaultModel("gemini-2.5-flash"),
		googleai.WithRest(), googleai.WithHTTPClient(c.client.NewHTTPClient(crystil.ProviderGoogle, httpClient)), endpoint)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := llms.GenerateFromSinglePrompt(ctx, llm, "What is 2 + 2?"); err != nil {
		t.Fatal(err)
	}
	if key != "test-key" {
		t.Errorf("API key = %q, want the one Google's client puts on each request", key)
	}
	if got := get(c.payload(t), "conversation.client.title"); got != "chatgooglegenai" {
		t.Errorf("client title = %v, want chatgooglegenai", got)
	}
}
