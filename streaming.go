package payloop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/PayloopAI/go-sdk/internal"
)

// streamingResponseBody wraps a streaming response body to accumulate chunks for analytics.
// It reads transparently while building a complete response for tracking.
type streamingResponseBody struct {
	reader    io.ReadCloser
	buffer    *bytes.Buffer
	client    *Client
	provider  Provider
	reqBody   []byte
	startTime time.Time
	ctx       context.Context
}

// newStreamingResponseBody creates a streaming wrapper.
func newStreamingResponseBody(
	reader io.ReadCloser,
	client *Client,
	provider Provider,
	reqBody []byte,
	startTime time.Time,
	ctx context.Context,
) *streamingResponseBody {
	return &streamingResponseBody{
		reader:    reader,
		buffer:    &bytes.Buffer{},
		client:    client,
		provider:  provider,
		reqBody:   reqBody,
		startTime: startTime,
		ctx:       ctx,
	}
}

// Read implements io.Reader, accumulating chunks as they're read.
func (s *streamingResponseBody) Read(p []byte) (n int, err error) {
	n, err = s.reader.Read(p)
	if n > 0 {
		s.buffer.Write(p[:n])
	}
	return n, err
}

// Close implements io.Closer, sending analytics when stream completes.
func (s *streamingResponseBody) Close() error {
	defer s.reader.Close()

	// Reconstruct complete response from SSE chunks
	response := s.reconstructResponse()

	// Parse request
	var query map[string]interface{}
	if len(s.reqBody) > 0 {
		_ = json.Unmarshal(s.reqBody, &query)
	}

	// Build attribution for payload (read from client dynamically)
	var attr interface{}
	if s.client.attribution != nil {
		attr = map[string]interface{}{
			"parent": map[string]interface{}{
				"id":   s.client.attribution.Parent.ID,
				"name": s.client.attribution.Parent.Name,
			},
		}
		if s.client.attribution.Subsidiary != nil {
			attr.(map[string]interface{})["subsidiary"] = map[string]interface{}{
				"id":   s.client.attribution.Subsidiary.ID,
				"name": s.client.attribution.Subsidiary.Name,
			}
		}
	}

	// Build and send analytics payload
	payload := internal.Payload{
		Attribution: attr,
		Conversation: internal.Conversation{
			Client: internal.ClientInfo{
				Provider: "",
				Title:    string(s.provider),
				Version:  "",
			},
			Query:    query,
			Response: response,
		},
		Meta: internal.Meta{
			API: internal.APIInfo{
				Key: s.client.cfg.apiKey,
			},
			FNFG: internal.FNFGInfo{
				Exc:    nil,
				Status: "succeeded",
			},
			SDK: internal.SDKInfo{
				Client:  "go",
				Version: s.client.cfg.version,
			},
		},
		Time: internal.TimeInfo{
			Start: s.startTime,
			End:   time.Now(),
		},
		Tx: internal.Transaction{
			UUID: s.client.txID,
		},
	}

	s.client.collector.SendAsync(s.ctx, payload)
	return nil
}

// reconstructResponse parses SSE chunks and builds a complete response object.
// Supports OpenAI, Anthropic, and Google streaming formats.
func (s *streamingResponseBody) reconstructResponse() map[string]interface{} {
	content := s.buffer.String()
	scanner := bufio.NewScanner(strings.NewReader(content))

	var chunks []map[string]interface{}
	var reconstructed map[string]interface{}

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}"
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Skip [DONE] markers
			if data == "[DONE]" {
				continue
			}

			// Parse chunk
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				chunks = append(chunks, chunk)
			}
		}
	}

	if len(chunks) == 0 {
		return map[string]interface{}{}
	}

	// Start with the first chunk as the base
	reconstructed = make(map[string]interface{})
	for k, v := range chunks[0] {
		reconstructed[k] = v
	}

	// Merge chunks based on provider format
	if s.provider == ProviderOpenAI {
		reconstructed = s.reconstructOpenAI(chunks)
	} else if s.provider == ProviderAnthropic {
		reconstructed = s.reconstructAnthropic(chunks)
	} else if s.provider == ProviderGoogle {
		reconstructed = s.reconstructGoogle(chunks)
	}

	return reconstructed
}

// reconstructOpenAI merges OpenAI streaming chunks.
func (s *streamingResponseBody) reconstructOpenAI(chunks []map[string]interface{}) map[string]interface{} {
	if len(chunks) == 0 {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})
	for k, v := range chunks[0] {
		result[k] = v
	}

	// Accumulate content from all chunks
	var content strings.Builder
	var role string

	for _, chunk := range chunks {
		if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if r, ok := delta["role"].(string); ok {
						role = r
					}
					if c, ok := delta["content"].(string); ok {
						content.WriteString(c)
					}
				}
			}
		}
	}

	// Build final message format
	if role != "" || content.Len() > 0 {
		result["choices"] = []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    role,
					"content": content.String(),
				},
				"finish_reason": "stop",
				"index":         0,
			},
		}
	}

	return result
}

// reconstructAnthropic merges Anthropic streaming chunks.
func (s *streamingResponseBody) reconstructAnthropic(chunks []map[string]interface{}) map[string]interface{} {
	if len(chunks) == 0 {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})
	var content strings.Builder

	// Find message_start event for base structure
	for _, chunk := range chunks {
		if eventType, ok := chunk["type"].(string); ok && eventType == "message_start" {
			if message, ok := chunk["message"].(map[string]interface{}); ok {
				for k, v := range message {
					result[k] = v
				}
			}
			break
		}
	}

	// Accumulate content from content_block_delta events
	for _, chunk := range chunks {
		if eventType, ok := chunk["type"].(string); ok {
			if eventType == "content_block_delta" {
				if delta, ok := chunk["delta"].(map[string]interface{}); ok {
					if text, ok := delta["text"].(string); ok {
						content.WriteString(text)
					}
				}
			}
		}
	}

	// Build final content array
	if content.Len() > 0 {
		result["content"] = []map[string]interface{}{
			{
				"type": "text",
				"text": content.String(),
			},
		}
	}

	return result
}

// reconstructGoogle merges Google GenAI streaming chunks.
func (s *streamingResponseBody) reconstructGoogle(chunks []map[string]interface{}) map[string]interface{} {
	if len(chunks) == 0 {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})
	for k, v := range chunks[0] {
		result[k] = v
	}

	// Accumulate content from all chunks
	var content strings.Builder

	for _, chunk := range chunks {
		if candidates, ok := chunk["candidates"].([]interface{}); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]interface{}); ok {
				if contentObj, ok := candidate["content"].(map[string]interface{}); ok {
					if parts, ok := contentObj["parts"].([]interface{}); ok {
						for _, part := range parts {
							if partMap, ok := part.(map[string]interface{}); ok {
								if text, ok := partMap["text"].(string); ok {
									content.WriteString(text)
								}
							}
						}
					}
				}
			}
		}
	}

	// Build final candidates format
	if content.Len() > 0 {
		result["candidates"] = []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]interface{}{
						{
							"text": content.String(),
						},
					},
					"role": "model",
				},
				"finishReason": "STOP",
			},
		}
	}

	return result
}
