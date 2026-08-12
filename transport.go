package crystil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/CrystilAI/go-sdk/internal"
)

// tracingTransport wraps http.RoundTripper to capture LLM calls.
type tracingTransport struct {
	base     http.RoundTripper
	client   *Client
	provider Provider
}

// RoundTrip executes the request and captures analytics.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Capture request body
	reqBody, err := captureBody(req.Body)
	if err != nil {
		return nil, fmt.Errorf("crystil: capture request: %w", err)
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
			t.client,
			t.provider,
			reqBody,
			start,
			req.Context(),
		)
	} else {
		// Non-streaming: capture complete response
		respBody, err := captureBody(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("crystil: capture response: %w", err)
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		// Build and send analytics payload async
		payload := t.buildPayload(reqBody, respBody, start, time.Now())
		t.client.collector.SendAsync(req.Context(), payload)
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

	// Build attribution for payload (read from client dynamically)
	var attr interface{}
	if t.client.attribution != nil {
		parentMap := map[string]interface{}{
			"id": t.client.attribution.ParentID,
		}
		if t.client.attribution.ParentName != nil {
			parentMap["name"] = *t.client.attribution.ParentName
		}

		attr = map[string]interface{}{
			"parent": parentMap,
		}

		if t.client.attribution.SubsidiaryID != nil {
			subsidiaryMap := map[string]interface{}{
				"id": *t.client.attribution.SubsidiaryID,
			}
			if t.client.attribution.SubsidiaryName != nil {
				subsidiaryMap["name"] = *t.client.attribution.SubsidiaryName
			}
			attr.(map[string]interface{})["subsidiary"] = subsidiaryMap
		}
	}

	return internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    string(t.provider),
				Version:  "",
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: t.client.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: t.client.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: start,
			End:   end,
		},
		Tx: internal.Transaction{
			UUID: t.client.txID,
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
