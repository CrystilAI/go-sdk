package payloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/PayloopAI/go-sdk/internal"
)

// tracingTransport wraps http.RoundTripper to capture LLM calls.
type tracingTransport struct {
	base        http.RoundTripper
	collector   *collector
	cfg         *config
	txID        string
	attribution *Attribution
	provider    string
	title       string
	version     string
}

// RoundTrip executes the request and captures analytics.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Capture request body
	reqBody, err := captureBody(req.Body)
	if err != nil {
		return nil, fmt.Errorf("payloop: capture request: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(reqBody))

	// Check if this is a streaming request
	isStreaming := isStreamingRequest(reqBody)

	// Execute request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Handle streaming vs non-streaming
	if isStreaming {
		// Wrap response body with streaming handler
		resp.Body = newStreamingResponseBody(
			resp.Body,
			t.collector,
			t.cfg,
			t.txID,
			t.attribution,
			t.provider,
			t.title,
			t.version,
			reqBody,
			start,
			req.Context(),
		)
	} else {
		// Non-streaming: capture complete response
		respBody, err := captureBody(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("payloop: capture response: %w", err)
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// Build and send analytics payload async
		payload := t.buildPayload(reqBody, respBody, start, time.Now())
		t.collector.SendAsync(req.Context(), payload)
	}

	return resp, nil
}

// buildPayload constructs the analytics payload.
func (t *tracingTransport) buildPayload(reqBody, respBody []byte, start, end time.Time) internal.Payload {
	var query map[string]interface{}
	var response map[string]interface{}

	// Parse request and response bodies
	if len(reqBody) > 0 {
		_ = json.Unmarshal(reqBody, &query)
	}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &response)
	}

	// Build attribution for payload
	var attr interface{}
	if t.attribution != nil {
		attr = map[string]interface{}{
			"parent": map[string]interface{}{
				"id":   t.attribution.Parent.ID,
				"name": t.attribution.Parent.Name,
			},
		}
		if t.attribution.Subsidiary != nil {
			attr.(map[string]interface{})["subsidiary"] = map[string]interface{}{
				"id":   t.attribution.Subsidiary.ID,
				"name": t.attribution.Subsidiary.Name,
			}
		}
	}

	return internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: t.provider,
				Title:    t.title,
				Version:  t.version,
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: t.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: t.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: start,
			End:   end,
		},
		Tx: internal.Transaction{
			UUID: t.txID,
		},
	}
}

// captureBody reads and returns the body content.
func captureBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	return io.ReadAll(body)
}

// isStreamingRequest checks if the request body indicates streaming.
func isStreamingRequest(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	// Check for "stream": true
	if stream, ok := req["stream"].(bool); ok && stream {
		return true
	}

	return false
}
