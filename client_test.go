package payloop

import (
	"context"
	"os"
	"testing"
	"time"

	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	openaiOption "github.com/openai/openai-go/v3/option"
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
		attr    Attribution
		wantErr bool
	}{
		{
			name: "valid parent only",
			attr: Attribution{
				Parent: AttributionEntity{
					ID:   "user-123",
					Name: "John Doe",
				},
			},
			wantErr: false,
		},
		{
			name: "valid parent and subsidiary",
			attr: Attribution{
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
			attr: Attribution{
				Parent: AttributionEntity{
					Name: "John Doe",
				},
			},
			wantErr: true,
		},
		{
			name: "parent ID too long",
			attr: Attribution{
				Parent: AttributionEntity{
					ID: string(make([]byte, 101)),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newClient, err := client.SetAttribution(tt.attr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetAttribution() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && newClient.attribution == nil {
				t.Error("Expected attribution to be set")
			}

			if client.attribution != nil {
				t.Error("Original client's attribution was modified")
			}
		})
	}
}

func TestRegisterOpenAI(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	openaiClient := client.RegisterOpenAI(ctx, openaiOption.WithAPIKey("sk-test-openai-key"))
	if openaiClient == nil {
		t.Error("Expected OpenAI client to be non-nil")
	}
}

func TestWrapOpenAIClient(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	openaiClient := client.WrapOpenAIClient(ctx, "sk-test-openai-key")
	if openaiClient == nil {
		t.Error("Expected OpenAI client to be non-nil")
	}
}

func TestRegisterAnthropic(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	// RegisterAnthropic should not panic and should return successfully
	_ = client.RegisterAnthropic(ctx, anthropicOption.WithAPIKey("test-anthropic-key"))
}

func TestWrapAnthropicClient(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	// WrapAnthropicClient should not panic and should return successfully
	_ = client.WrapAnthropicClient(ctx, "test-anthropic-key")
}

func TestRegisterAnthropicWithMultipleOptions(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	// RegisterAnthropic should accept multiple options without panicking
	_ = client.RegisterAnthropic(
		ctx,
		anthropicOption.WithAPIKey("test-anthropic-key"),
		anthropicOption.WithMaxRetries(3),
	)
}

func TestProviderRegistrationWithAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Set attribution
	client, err = client.SetAttribution(Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Test OpenAI with attribution
	openaiClient := client.WrapOpenAIClient(ctx, "sk-test-key")
	if openaiClient == nil {
		t.Error("Expected OpenAI client with attribution to be non-nil")
	}

	// Test Anthropic with attribution - should not panic
	_ = client.WrapAnthropicClient(ctx, "test-key")
}

func TestProviderRegistrationWithTransaction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Create new transaction
	txClient := client.NewTransaction()
	if txClient.txID == client.txID {
		t.Error("Expected different transaction ID")
	}

	ctx := context.Background()

	// Test OpenAI with new transaction
	openaiClient := txClient.WrapOpenAIClient(ctx, "sk-test-key")
	if openaiClient == nil {
		t.Error("Expected OpenAI client with new transaction to be non-nil")
	}

	// Test Anthropic with new transaction - should not panic
	_ = txClient.WrapAnthropicClient(ctx, "test-key")
}

func TestRegisterGoogle(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	googleClient, err := client.RegisterGoogle(ctx, &genai.ClientConfig{
		APIKey:  "test-google-key",
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		t.Fatalf("RegisterGoogle() error = %v", err)
	}
	if googleClient == nil {
		t.Error("Expected Google client to be non-nil")
	}
}

func TestWrapGoogleClient(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	googleClient, err := client.WrapGoogleClient(ctx, "test-google-key")
	if err != nil {
		t.Fatalf("WrapGoogleClient() error = %v", err)
	}
	if googleClient == nil {
		t.Error("Expected Google client to be non-nil")
	}
}

func TestRegisterGoogleWithAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Set attribution
	client, err = client.SetAttribution(Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	googleClient, err := client.WrapGoogleClient(ctx, "test-google-key")
	if err != nil {
		t.Fatalf("WrapGoogleClient() with attribution error = %v", err)
	}
	if googleClient == nil {
		t.Error("Expected Google client with attribution to be non-nil")
	}
}

func TestRegisterGoogleWithTransaction(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Create new transaction
	txClient := client.NewTransaction()
	if txClient.txID == client.txID {
		t.Error("Expected different transaction ID")
	}

	ctx := context.Background()
	googleClient, err := txClient.WrapGoogleClient(ctx, "test-google-key")
	if err != nil {
		t.Fatalf("WrapGoogleClient() with new transaction error = %v", err)
	}
	if googleClient == nil {
		t.Error("Expected Google client with new transaction to be non-nil")
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

func TestNewLangChainHandlerWithAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Set attribution
	client, err = client.SetAttribution(Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
		Subsidiary: &AttributionEntity{
			ID:   "team-456",
			Name: "Engineering",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := client.NewLangChainHandler()
	if handler == nil {
		t.Error("Expected non-nil LangChain handler with attribution")
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
