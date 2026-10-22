package providertest

import (
	"context"
	"testing"

	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

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
