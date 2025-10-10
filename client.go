package payloop

import (
	"net/http"
	"os"
	"time"

	"github.com/PayloopAI/go-sdk/api"
	"github.com/google/uuid"
	"github.com/tmc/langchaingo/callbacks"
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

// SetAttribution sets attribution on the client, affecting all future requests.
// This method mutates the client in place.
func (c *Client) SetAttribution(attr *Attribution) error {
	if attr != nil {
		if err := attr.validate(); err != nil {
			return err
		}
	}
	c.attribution = attr
	return nil
}

// HTTPClient returns an HTTP client configured to track analytics for the specified provider.
// Use this client when creating your own provider clients (OpenAI, Anthropic, Google, etc.).
//
// Example:
//
//	payloop, _ := payloop.New(payloop.WithAPIKey("..."))
//	httpClient := payloop.HTTPClient(payloop.ProviderOpenAI)
//	openaiClient := openai.NewClient(openai.WithHTTPClient(httpClient), openai.WithAPIKey("..."))
func (c *Client) HTTPClient(provider Provider) *http.Client {
	return &http.Client{
		Transport: &tracingTransport{
			base:     http.DefaultTransport,
			client:   c,
			provider: provider,
		},
	}
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

// NewLangChainHandler creates a LangChain callback handler with Payloop analytics tracking.
// Use this with LangChain's callback system to automatically track LLM calls.
func (c *Client) NewLangChainHandler() callbacks.Handler {
	return newLangChainHandler(c)
}
