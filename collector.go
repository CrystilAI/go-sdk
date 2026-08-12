package crystil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/CrystilAI/go-sdk/internal"
)

// collector sends analytics asynchronously.
type collector struct {
	client   *http.Client
	endpoint string
	cfg      *config

	// Channel for fire-and-forget sends
	queue chan internal.Payload
	done  chan struct{}
}

// newCollector creates a new collector instance.
func newCollector(cfg *config) *collector {
	c := &collector{
		client: &http.Client{
			Timeout:   cfg.timeout,
			Transport: &retryTransport{base: http.DefaultTransport, maxRetries: 5},
		},
		endpoint: cfg.collectorURL + "/rec",
		cfg:      cfg,
		queue:    make(chan internal.Payload, 100), // buffered for rate limiting
		done:     make(chan struct{}),
	}

	go c.worker() // Single worker goroutine
	return c
}

// SendAsync queues analytics for async submission.
func (c *collector) SendAsync(ctx context.Context, p internal.Payload) {
	select {
	case c.queue <- p:
	case <-ctx.Done():
	default:
		// Channel full, drop to prevent blocking
	}
}

// worker processes analytics submissions.
func (c *collector) worker() {
	for p := range c.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = c.send(ctx, p) // Fire and forget
		cancel()
	}
	close(c.done)
}

// send transmits a single payload to the collector.
func (c *collector) send(ctx context.Context, p internal.Payload) error {
	// Skip in test mode
	if os.Getenv("CRYSTIL_TEST_MODE") != "" {
		return nil
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error: %d", resp.StatusCode)
	}

	return nil
}

// Close gracefully shuts down the collector.
func (c *collector) Close() error {
	close(c.queue)
	<-c.done
	return nil
}

// retryTransport implements exponential backoff retry for 5xx errors.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
}

// RoundTrip executes a request with retry logic.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	maxRetries := t.maxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}

	var resp *http.Response
	var err error

	for i := 0; i < maxRetries; i++ {
		resp, err = t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		// Retry on 5xx errors
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			resp.Body.Close()
			backoff := time.Duration(1<<uint(i)) * time.Second
			time.Sleep(backoff)
			continue
		}

		return resp, nil
	}

	return resp, nil
}
