package payloop

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/PayloopAI/go-sdk/api"
	"github.com/anthropics/anthropic-sdk-go"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	openaiOption "github.com/openai/openai-go/v3/option"
	"github.com/tmc/langchaingo/callbacks"
	"google.golang.org/genai"
)

const version = "0.1.0"

// Client tracks LLM costs across providers.
type Client struct {
	cfg         *config
	collector   *collector
	txID        string
	attribution *Attribution
}

// New creates a Payloop client. If no API key is provided via options,
// it reads from PAYLOOP_API_KEY environment variable.
func New(opts ...Option) (*Client, error) {
	cfg := &config{
		collectorURL:      "https://collector.trypayloop.com",
		apiURL:            "https://api.trypayloop.com",
		timeout:           5 * time.Second,
		raiseFinalAttempt: true,
		version:           version,
	}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("PAYLOOP_API_KEY")
	}

	if cfg.apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	return &Client{
		cfg:       cfg,
		collector: newCollector(cfg),
		txID:      uuid.New().String(),
	}, nil
}

// NewTransaction returns a client with a fresh transaction ID.
// The original client is unmodified (value semantics).
func (c *Client) NewTransaction() *Client {
	cp := *c
	cp.txID = uuid.New().String()
	return &cp
}

// SetAttribution returns a client with attribution set.
// The original client is unmodified.
func (c *Client) SetAttribution(attr Attribution) (*Client, error) {
	if err := attr.validate(); err != nil {
		return nil, err
	}

	cp := *c
	cp.attribution = &attr
	return &cp, nil
}

// Close gracefully shuts down the client, ensuring all analytics are sent.
func (c *Client) Close() error {
	return c.collector.Close()
}

// Workflows returns a client for managing workflows.
func (c *Client) Workflows() *api.WorkflowsClient {
	apiClient := api.NewClient(c.cfg.apiURL, c.cfg.apiKey, c.cfg.timeout)
	return api.NewWorkflowsClient(apiClient)
}

// Workflow returns a client for managing a specific workflow.
func (c *Client) Workflow(uuid string) *api.WorkflowClient {
	apiClient := api.NewClient(c.cfg.apiURL, c.cfg.apiKey, c.cfg.timeout)
	return api.NewWorkflowClient(uuid, apiClient)
}

// RegisterOpenAI wraps an OpenAI client to track costs.
// Returns a new client with analytics tracking enabled.
func (c *Client) RegisterOpenAI(ctx context.Context, opts ...openaiOption.RequestOption) *openai.Client {
	// Create HTTP client with our tracing transport
	httpClient := &http.Client{
		Transport: &tracingTransport{
			base:        http.DefaultTransport,
			collector:   c.collector,
			cfg:         c.cfg,
			txID:        c.txID,
			attribution: c.attribution,
			provider:    "",
			title:       "openai",
			version:     "",
		},
	}

	// Prepend the HTTP client option so user options can override if needed
	allOpts := append([]openaiOption.RequestOption{openaiOption.WithHTTPClient(httpClient)}, opts...)

	client := openai.NewClient(allOpts...)
	return &client
}

// WrapOpenAIClient is a convenience method that creates an OpenAI client with the given API key.
// This is equivalent to RegisterOpenAI with openaiOption.WithAPIKey.
func (c *Client) WrapOpenAIClient(ctx context.Context, apiKey string) *openai.Client {
	return c.RegisterOpenAI(ctx, openaiOption.WithAPIKey(apiKey))
}

// RegisterAnthropic creates an Anthropic client with analytics tracking enabled.
// It wraps the HTTP transport to capture request/response data.
func (c *Client) RegisterAnthropic(ctx context.Context, opts ...anthropicOption.RequestOption) anthropic.Client {
	// Create HTTP client with our tracing transport
	httpClient := &http.Client{
		Transport: &tracingTransport{
			base:        http.DefaultTransport,
			collector:   c.collector,
			cfg:         c.cfg,
			txID:        c.txID,
			attribution: c.attribution,
			provider:    "",
			title:       "anthropic",
			version:     "",
		},
	}

	// Prepend the HTTP client option so user options can override if needed
	allOpts := append([]anthropicOption.RequestOption{anthropicOption.WithHTTPClient(httpClient)}, opts...)

	return anthropic.NewClient(allOpts...)
}

// WrapAnthropicClient is a convenience method that creates an Anthropic client with the given API key.
// This is equivalent to RegisterAnthropic with anthropicOption.WithAPIKey.
func (c *Client) WrapAnthropicClient(ctx context.Context, apiKey string) anthropic.Client {
	return c.RegisterAnthropic(ctx, anthropicOption.WithAPIKey(apiKey))
}

// RegisterGoogle creates a Google GenAI client with analytics tracking enabled.
// It wraps the HTTP transport to capture request/response data.
func (c *Client) RegisterGoogle(ctx context.Context, config *genai.ClientConfig) (*genai.Client, error) {
	// Create HTTP client with our tracing transport
	httpClient := &http.Client{
		Transport: &tracingTransport{
			base:        http.DefaultTransport,
			collector:   c.collector,
			cfg:         c.cfg,
			txID:        c.txID,
			attribution: c.attribution,
			provider:    "",
			title:       "google",
			version:     "",
		},
	}

	// Initialize config if nil
	if config == nil {
		config = &genai.ClientConfig{}
	}

	// Set our HTTP client if not already provided
	if config.HTTPClient == nil {
		config.HTTPClient = httpClient
	}

	return genai.NewClient(ctx, config)
}

// WrapGoogleClient is a convenience method that creates a Google GenAI client with the given API key.
// This is equivalent to RegisterGoogle with a ClientConfig containing the API key.
func (c *Client) WrapGoogleClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	return c.RegisterGoogle(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
}

// NewLangChainHandler creates a LangChain callback handler with Payloop analytics tracking.
// Use this with LangChain's callback system to automatically track LLM calls.
func (c *Client) NewLangChainHandler() callbacks.Handler {
	return newLangChainHandler(c.collector, c.cfg, c.txID, c.attribution)
}
