package crystil

import (
	"net/http"
	"os"
	"time"

	"github.com/CrystilAI/go-sdk/api"
	"github.com/google/uuid"
)

const version = "0.2.1"

// Client tracks LLM costs across providers.
type Client struct {
	cfg         *config
	collector   *collector
	txID        string
	attribution *Attribution
}

// New creates a Crystil client. If no API key is provided via options,
// it reads from CRYSTIL_API_KEY environment variable. The collector and API
// URLs come from WithCollectorURL and WithAPIURL, then from the
// CRYSTIL_COLLECTOR_URL_BASE and CRYSTIL_API_URL_BASE environment variables,
// then default to production.
func New(opts ...Option) (*Client, error) {
	cfg := &config{
		collectorURL:      envOr("CRYSTIL_COLLECTOR_URL_BASE", "https://collector.crystil.com"),
		apiURL:            envOr("CRYSTIL_API_URL_BASE", "https://api.crystil.com"),
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
		cfg.apiKey = os.Getenv("CRYSTIL_API_KEY")
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
// Its only setting is its Transport, from WrapTransport(provider, nil).
//
// Example:
//
//	crystilClient, _ := crystil.New(crystil.WithAPIKey("..."))
//	httpClient := crystilClient.HTTPClient(crystil.ProviderOpenAI)
//	openaiClient := openai.NewClient(option.WithHTTPClient(httpClient), option.WithAPIKey("..."))
func (c *Client) HTTPClient(provider Provider) *http.Client {
	return &http.Client{Transport: c.WrapTransport(provider, nil)}
}

// NewHTTPClient returns a copy of base, with the same settings, that tracks
// analytics for the specified provider and sends each request on through base's
// transport. base itself is left as it was; a nil base gives what HTTPClient does.
// Use it where the provider's library needs an HTTP client of its own, such as
// Google's authenticated client for Vertex AI.
//
// Example:
//
//	httpClient, err := httptransport.NewClient(&httptransport.Options{Credentials: creds})
//	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
//		Backend:    genai.BackendVertexAI,
//		HTTPClient: crystilClient.NewHTTPClient(crystil.ProviderGoogle, httpClient),
//	})
func (c *Client) NewHTTPClient(provider Provider, base *http.Client) *http.Client {
	if base == nil {
		return c.HTTPClient(provider)
	}
	httpClient := *base
	httpClient.Transport = c.WrapTransport(provider, base.Transport)
	return &httpClient
}

// WrapTransport returns a transport that tracks analytics for the specified
// provider and sends each request on through base, or http.DefaultTransport when
// base is nil. Use it where a library takes a transport rather than a client.
func (c *Client) WrapTransport(provider Provider, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &tracingTransport{base: base, client: c, provider: provider}
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
