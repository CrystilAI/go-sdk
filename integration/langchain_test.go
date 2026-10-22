//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/openai"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
)

func TestLangChainOpenAI(t *testing.T) {
	m := model("OPENAI_MODEL", "gpt-4o-mini")
	testLangChain(t, "OPENAI_API_KEY", "chatopenai", m, checkChatStream, func(t *testing.T, client *crystil.Client) llms.Model {
		llm, err := openai.New(
			openai.WithModel(m),
			openai.WithToken(os.Getenv("OPENAI_API_KEY")),
			openai.WithHTTPClient(client.HTTPClient(crystil.ProviderOpenAI)),
		)
		if err != nil {
			t.Fatal(err)
		}
		return llm
	})
}

// TestLangChainGoogle wires the API key as examples/langchain-google does: on
// each request, since Google's library adds none to an HTTP client it is given.
// The request names the model as "models/...", a prefix the backend strips.
func TestLangChainGoogle(t *testing.T) {
	m := model("GOOGLE_MODEL", "gemini-2.5-flash")
	testLangChain(t, "GOOGLE_API_KEY", "chatgooglegenai", "models/"+m, checkEventList, func(t *testing.T, client *crystil.Client) llms.Model {
		apiKey := os.Getenv("GOOGLE_API_KEY")
		httpClient, _, err := htransport.NewClient(t.Context(), option.WithAPIKey(apiKey))
		if err != nil {
			t.Fatal(err)
		}
		llm, err := googleai.New(t.Context(),
			googleai.WithAPIKey(apiKey),
			googleai.WithDefaultModel(m),
			googleai.WithRest(),
			googleai.WithHTTPClient(client.NewHTTPClient(crystil.ProviderGoogle, httpClient)),
		)
		if err != nil {
			t.Fatal(err)
		}
		return llm
	})
}

// testLangChain makes a plain and a streamed call through the langchaingo LLM
// newLLM builds, and checks they are recorded as LangChain calls.
func testLangChain(t *testing.T, key, title, queryModel string, checkStream func(*testing.T, map[string]any),
	newLLM func(*testing.T, *crystil.Client) llms.Model) {
	cases := []struct {
		name string
		opts []llms.CallOption
	}{
		{"generate", nil},
		{"generate stream", []llms.CallOption{llms.WithStreamingFunc(func(context.Context, []byte) error { return nil })}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, client, rec := start(t, key)
			answer, err := llms.GenerateFromSinglePrompt(ctx, newLLM(t, client), userPrompt, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if answer == "" {
				t.Fatal("model gave no answer")
			}

			p := rec.payload(t)
			checkPayload(t, p, title, queryModel)
			if got := get(p, "conversation.client.provider"); got != "langchain" {
				t.Errorf("conversation.client.provider = %v, want langchain", got)
			}
			if len(tc.opts) > 0 {
				checkStream(t, p)
			}
		})
	}
}
