package crystil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
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

	mu     sync.RWMutex // guards closed, so nothing sends on a closed queue
	closed bool
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

// SendAsync queues analytics for async submission, and drops them once the
// collector is closed. It takes no context: a call whose context has ended, as
// when the end user disconnects, still happened, so it is still recorded.
func (c *collector) SendAsync(p internal.Payload) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return
	}
	select {
	case c.queue <- p:
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

// Close gracefully shuts down the collector. Calling it again does nothing.
func (c *collector) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.queue)
	}
	c.mu.Unlock()
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
