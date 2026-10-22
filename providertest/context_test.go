package providertest

import (
	"context"
	"net/http"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	lcopenai "github.com/tmc/langchaingo/llms/openai"
	googleoption "google.golang.org/api/option"
	"google.golang.org/genai"
)

// TestContextTransactionAndAttribution checks that each library passes the
// call's context to its request, so a call is recorded under the transaction and
// attribution its context carries. A call whose context has no transaction is
// recorded under the client's, with no attribution.
func TestContextTransactionAndAttribution(t *testing.T) {
	server := newProviderServer(t)
	chat := openai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
	// Each library's client is built once, and its calls made with each context.
	libraries := []struct {
		name     string
		provider crystil.Provider
		newCall  func(t *testing.T, httpClient *http.Client) func(context.Context) error
	}{
		{"openai-go", crystil.ProviderOpenAI, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
			return func(ctx context.Context) error {
				_, err := ai.Chat.Completions.New(ctx, chat)
				return err
			}
		}},
		{"openai-go stream", crystil.ProviderOpenAI, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			ai := openai.NewClient(option.WithHTTPClient(httpClient), option.WithBaseURL(server.URL), option.WithAPIKey("test-key"))
			return func(ctx context.Context) error {
				s := ai.Chat.Completions.NewStreaming(ctx, chat)
				for s.Next() {
				}
				return s.Err()
			}
		}},
		{"anthropic-sdk-go", crystil.ProviderAnthropic, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			ai := anthropic.NewClient(anthropicoption.WithHTTPClient(httpClient),
				anthropicoption.WithBaseURL(server.URL), anthropicoption.WithAPIKey("test-key"))
			return func(ctx context.Context) error {
				_, err := ai.Messages.New(ctx, anthropic.MessageNewParams{
					Model:     "claude-haiku-4-5",
					MaxTokens: 10,
					Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
				})
				return err
			}
		}},
		{"genai", crystil.ProviderGoogle, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			gc, err := genai.NewClient(context.Background(), &genai.ClientConfig{
				Backend:     genai.BackendGeminiAPI,
				APIKey:      "test-key",
				HTTPClient:  httpClient,
				HTTPOptions: genai.HTTPOptions{BaseURL: server.URL},
			})
			if err != nil {
				t.Fatal(err)
			}
			return func(ctx context.Context) error {
				_, err := gc.Models.GenerateContent(ctx, "gemini-2.0-flash", genai.Text("hi"), nil)
				return err
			}
		}},
		{"langchaingo", crystil.ProviderOpenAI, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			llm, err := lcopenai.New(lcopenai.WithModel("gpt-4o-mini"), lcopenai.WithToken("test-key"),
				lcopenai.WithBaseURL(server.URL), lcopenai.WithHTTPClient(httpClient))
			if err != nil {
				t.Fatal(err)
			}
			return func(ctx context.Context) error {
				_, err := llms.GenerateFromSinglePrompt(ctx, llm, "hi")
				return err
			}
		}},
		{"langchaingo google", crystil.ProviderGoogle, func(t *testing.T, httpClient *http.Client) func(context.Context) error {
			endpoint := func(o *googleai.Options) {
				o.ClientOptions = append(o.ClientOptions, googleoption.WithEndpoint(server.URL))
			}
			llm, err := googleai.New(context.Background(), googleai.WithAPIKey("test-key"),
				googleai.WithDefaultModel("gemini-2.5-flash"), googleai.WithRest(), googleai.WithHTTPClient(httpClient), endpoint)
			if err != nil {
				t.Fatal(err)
			}
			return func(ctx context.Context) error {
				_, err := llms.GenerateFromSinglePrompt(ctx, llm, "hi")
				return err
			}
		}},
	}

	attributed := func(t *testing.T, ctx context.Context, parentID string) context.Context {
		ctx, err := crystil.SetAttribution(ctx, &crystil.Attribution{ParentID: parentID})
		if err != nil {
			t.Fatal(err)
		}
		return ctx
	}
	for _, lib := range libraries {
		t.Run(lib.name, func(t *testing.T) {
			c := newCollector(t)
			call := lib.newCall(t, c.client.HTTPClient(lib.provider))

			first := attributed(t, crystil.NewTransaction(context.Background()), "first")
			calls := []struct {
				name   string
				ctx    context.Context
				tx     int // calls with the same number share a transaction
				parent any // nil when the call has no attribution
			}{
				{"no transaction", context.Background(), 0, nil},
				{"new transaction", first, 1, "first"},
				{"attribution changed", attributed(t, first, "changed"), 1, "changed"},
				{"another transaction", crystil.NewTransaction(first), 2, "first"},
			}
			for _, tc := range calls {
				if err := call(tc.ctx); err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			}

			got := c.payloads()
			if len(got) != len(calls) {
				t.Fatalf("collector got %d payloads, want %d", len(got), len(calls))
			}
			txs := map[int]any{}
			for i, tc := range calls {
				if parent := get(got[i], "attribution.parent.id"); parent != tc.parent {
					t.Errorf("%s: attribution.parent.id = %v, want %v", tc.name, parent, tc.parent)
				}
				tx := get(got[i], "tx.uuid")
				if want, ok := txs[tc.tx]; ok && tx != want {
					t.Errorf("%s: tx.uuid = %v, want the earlier call's, %v", tc.name, tx, want)
				}
				txs[tc.tx] = tx
			}
			if txs[0] == txs[1] || txs[1] == txs[2] || txs[0] == txs[2] {
				t.Errorf("tx.uuid by transaction = %v, want them all different", txs)
			}
		})
	}
}
