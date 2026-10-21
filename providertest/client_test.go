package providertest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"
)

func TestHTTPClientWithAttribution(t *testing.T) {
	client, err := crystil.New(crystil.WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	testUser := "Test User"
	err = client.SetAttribution(&crystil.Attribution{
		ParentID:   "user-123",
		ParentName: &testUser,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create OpenAI client - verifies HTTPClient works with OpenAI
	_ = openai.NewClient(
		option.WithHTTPClient(client.HTTPClient(crystil.ProviderOpenAI)),
		option.WithAPIKey("sk-test-key"),
	)
}

// TestHTTPClientMutableAttribution checks that the HTTP client reads the
// attribution on each request, so attribution set after the provider SDK's
// client was built applies to its later calls.
func TestHTTPClientMutableAttribution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-4o-mini",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	c := newCollector(t)
	ai := openai.NewClient(
		option.WithHTTPClient(c.client.HTTPClient(crystil.ProviderOpenAI)),
		option.WithBaseURL(server.URL),
		option.WithAPIKey("sk-test-key"),
	)
	ids := []string{"user-123", "user-456"}
	for _, id := range ids {
		if err := c.client.SetAttribution(&crystil.Attribution{ParentID: id}); err != nil {
			t.Fatal(err)
		}
		_, err := ai.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
			Model:    "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	got := c.payloads()
	if len(got) != len(ids) {
		t.Fatalf("collector got %d payloads, want %d", len(got), len(ids))
	}
	for i, id := range ids {
		if parent := get(got[i], "attribution.parent.id"); parent != id {
			t.Errorf("payload %d: attribution.parent.id = %v, want %s", i, parent, id)
		}
	}
}

func TestRegisterGoogle(t *testing.T) {
	client, err := crystil.New(crystil.WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	googleClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		HTTPClient: client.HTTPClient(crystil.ProviderGoogle),
		APIKey:     "test-google-key",
		Backend:    genai.BackendGeminiAPI,
	})
	if err != nil {
		t.Fatalf("genai.NewClient() error = %v", err)
	}
	if googleClient == nil {
		t.Error("Expected Google client to be non-nil")
	}
}
