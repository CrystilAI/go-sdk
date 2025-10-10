package payloop

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"google.golang.org/genai"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		setup   func()
		cleanup func()
		wantErr bool
	}{
		{
			name:    "no api key and no env var",
			opts:    []Option{},
			wantErr: true,
		},
		{
			name:    "with api key option",
			opts:    []Option{WithAPIKey("test-key")},
			wantErr: false,
		},
		{
			name: "from environment variable",
			opts: []Option{},
			setup: func() {
				os.Setenv("PAYLOOP_API_KEY", "env-test-key")
			},
			cleanup: func() {
				os.Unsetenv("PAYLOOP_API_KEY")
			},
			wantErr: false,
		},
		{
			name:    "empty api key",
			opts:    []Option{WithAPIKey("")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if tt.cleanup != nil {
				defer tt.cleanup()
			}

			client, err := New(tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				defer client.Close()
				if client.cfg.apiKey == "" {
					t.Error("Expected API key to be set")
				}
				if client.txID == "" {
					t.Error("Expected transaction ID to be set")
				}
			}
		})
	}
}

func TestWithTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "valid timeout",
			timeout: 10 * time.Second,
			wantErr: false,
		},
		{
			name:    "zero timeout",
			timeout: 0,
			wantErr: true,
		},
		{
			name:    "negative timeout",
			timeout: -5 * time.Second,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(
				WithAPIKey("test-key"),
				WithTimeout(tt.timeout),
			)
			if (err != nil) != tt.wantErr {
				t.Errorf("WithTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewTransaction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	originalTxID := client.txID

	newClient := client.NewTransaction()

	if newClient.txID == originalTxID {
		t.Error("Expected new transaction ID, got same ID")
	}

	if client.txID != originalTxID {
		t.Error("Original client's transaction ID was modified")
	}
}

func TestSetAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tests := []struct {
		name    string
		attr    *Attribution
		wantErr bool
	}{
		{
			name: "valid parent only",
			attr: &Attribution{
				Parent: AttributionEntity{
					ID:   "user-123",
					Name: "John Doe",
				},
			},
			wantErr: false,
		},
		{
			name: "valid parent and subsidiary",
			attr: &Attribution{
				Parent: AttributionEntity{
					ID:   "org-123",
					Name: "Acme Corp",
				},
				Subsidiary: &AttributionEntity{
					ID:   "team-456",
					Name: "Engineering",
				},
			},
			wantErr: false,
		},
		{
			name: "missing parent ID",
			attr: &Attribution{
				Parent: AttributionEntity{
					Name: "John Doe",
				},
			},
			wantErr: true,
		},
		{
			name: "parent ID too long",
			attr: &Attribution{
				Parent: AttributionEntity{
					ID: string(make([]byte, 101)),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SetAttribution(tt.attr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetAttribution() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && client.attribution == nil {
				t.Error("Expected attribution to be set")
			}
		})
	}
}

func TestHTTPClient(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tests := []struct {
		name     string
		provider Provider
	}{
		{"OpenAI", ProviderOpenAI},
		{"Anthropic", ProviderAnthropic},
		{"Google", ProviderGoogle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := client.HTTPClient(tt.provider)
			if httpClient == nil {
				t.Errorf("Expected non-nil HTTP client for %s", tt.provider)
			}
			if httpClient.Transport == nil {
				t.Errorf("Expected non-nil transport for %s", tt.provider)
			}
		})
	}
}

func TestHTTPClientWithAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Set attribution
	err = client.SetAttribution(&Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create HTTP client
	httpClient := client.HTTPClient(ProviderOpenAI)

	// Create OpenAI client - verifies HTTPClient works with OpenAI
	_ = openai.NewClient(
		option.WithHTTPClient(httpClient),
		option.WithAPIKey("sk-test-key"),
	)
}

func TestHTTPClientMutableAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Create HTTP client before setting attribution
	httpClient := client.HTTPClient(ProviderOpenAI)

	// Create OpenAI client - verifies HTTPClient works before attribution is set
	_ = openai.NewClient(
		option.WithHTTPClient(httpClient),
		option.WithAPIKey("sk-test-key"),
	)

	// Now set attribution - should affect future requests through the same httpClient
	err = client.SetAttribution(&Attribution{
		Parent: AttributionEntity{
			ID:   "user-456",
			Name: "Updated User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify attribution was set
	if client.attribution == nil {
		t.Error("Expected attribution to be set")
	}
	if client.attribution.Parent.ID != "user-456" {
		t.Errorf("Expected attribution ID user-456, got %s", client.attribution.Parent.ID)
	}
}

func TestRegisterGoogle(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	httpClient := client.HTTPClient(ProviderGoogle)

	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		HTTPClient: httpClient,
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

func TestNewLangChainHandler(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()
	if handler == nil {
		t.Error("Expected non-nil LangChain handler")
	}
}


func TestNewLangChainHandlerWithTransaction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Create new transaction
	txClient := client.NewTransaction()

	handler := txClient.NewLangChainHandler()
	if handler == nil {
		t.Error("Expected non-nil LangChain handler with transaction")
	}
}
