package crystil

import (
	"os"
	"testing"
	"time"
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
				os.Setenv("CRYSTIL_API_KEY", "env-test-key")
			},
			cleanup: func() {
				os.Unsetenv("CRYSTIL_API_KEY")
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

	johnDoe := "John Doe"
	acmeCorp := "Acme Corp"
	engineering := "Engineering"
	teamID := "team-456"

	tests := []struct {
		name    string
		attr    *Attribution
		wantErr bool
	}{
		{
			name: "valid parent only",
			attr: &Attribution{
				ParentID:   "user-123",
				ParentName: &johnDoe,
			},
			wantErr: false,
		},
		{
			name: "valid parent and subsidiary",
			attr: &Attribution{
				ParentID:       "org-123",
				ParentName:     &acmeCorp,
				SubsidiaryID:   &teamID,
				SubsidiaryName: &engineering,
			},
			wantErr: false,
		},
		{
			name: "missing parent ID",
			attr: &Attribution{
				ParentName: &johnDoe,
			},
			wantErr: true,
		},
		{
			name: "parent ID too long",
			attr: &Attribution{
				ParentID: string(make([]byte, 101)),
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
