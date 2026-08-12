package crystil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestIsStreamingRequest(t *testing.T) {
	tests := []struct {
		name     string
		reqBody  map[string]interface{}
		expected bool
	}{
		{
			name: "streaming enabled",
			reqBody: map[string]interface{}{
				"model":  "gpt-4",
				"stream": true,
			},
			expected: true,
		},
		{
			name: "streaming disabled",
			reqBody: map[string]interface{}{
				"model":  "gpt-4",
				"stream": false,
			},
			expected: false,
		},
		{
			name: "no stream field",
			reqBody: map[string]interface{}{
				"model": "gpt-4",
			},
			expected: false,
		},
		{
			name:     "empty body",
			reqBody:  map[string]interface{}{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.reqBody)
			result := isStreamingRequest(body)
			if result != tt.expected {
				t.Errorf("isStreamingRequest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsStreamingRequestInvalidJSON(t *testing.T) {
	result := isStreamingRequest([]byte("invalid json"))
	if result {
		t.Error("Expected isStreamingRequest to return false for invalid JSON")
	}
}

func TestStreamingResponseBodyRead(t *testing.T) {
	// Create a mock streaming response
	mockData := "data: {\"test\": \"chunk1\"}\n\ndata: {\"test\": \"chunk2\"}\n\n"
	reader := io.NopCloser(strings.NewReader(mockData))

	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	srb := newStreamingResponseBody(
		reader,
		client,
		ProviderOpenAI,
		[]byte(`{"model":"gpt-4","stream":true}`),
		time.Now(),
		context.Background(),
	)

	// Read the streaming data
	buf := make([]byte, 1024)
	n, err := srb.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}

	if n == 0 {
		t.Error("Expected to read some data")
	}

	// Verify data was buffered
	if srb.buffer.Len() == 0 {
		t.Error("Expected buffer to contain accumulated data")
	}

	// Close and verify analytics sent
	err = srb.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestReconstructOpenAI(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	srb := &streamingResponseBody{
		provider: ProviderOpenAI,
	}

	chunks := []map[string]interface{}{
		{
			"id":      "chatcmpl-123",
			"object":  "chat.completion.chunk",
			"created": 1234567890,
			"model":   "gpt-4",
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"delta": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello",
					},
				},
			},
		},
		{
			"id":      "chatcmpl-123",
			"object":  "chat.completion.chunk",
			"created": 1234567890,
			"model":   "gpt-4",
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"delta": map[string]interface{}{
						"content": " world",
					},
				},
			},
		},
	}

	result := srb.reconstructOpenAI(chunks)

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	choices, ok := result["choices"].([]map[string]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("Expected choices array in result")
	}

	message, ok := choices[0]["message"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected message in first choice")
	}

	content, ok := message["content"].(string)
	if !ok {
		t.Fatal("Expected content string in message")
	}

	if content != "Hello world" {
		t.Errorf("Expected content 'Hello world', got '%s'", content)
	}

	role, ok := message["role"].(string)
	if !ok || role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", role)
	}
}

func TestReconstructAnthropic(t *testing.T) {
	srb := &streamingResponseBody{
		provider: ProviderAnthropic,
	}

	chunks := []map[string]interface{}{
		{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":    "msg_123",
				"type":  "message",
				"role":  "assistant",
				"model": "claude-3-opus-20240229",
			},
		},
		{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": "Hello",
			},
		},
		{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": " from Claude",
			},
		},
	}

	result := srb.reconstructAnthropic(chunks)

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	content, ok := result["content"].([]map[string]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array in result")
	}

	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatal("Expected text string in content")
	}

	if text != "Hello from Claude" {
		t.Errorf("Expected text 'Hello from Claude', got '%s'", text)
	}
}

func TestReconstructGoogle(t *testing.T) {
	srb := &streamingResponseBody{
		provider: ProviderGoogle,
	}

	chunks := []map[string]interface{}{
		{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Hello",
							},
						},
					},
				},
			},
		},
		{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{
								"text": " from Gemini",
							},
						},
					},
				},
			},
		},
	}

	result := srb.reconstructGoogle(chunks)

	// Verify result structure
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	candidates, ok := result["candidates"].([]map[string]interface{})
	if !ok || len(candidates) == 0 {
		t.Fatal("Expected candidates array in result")
	}

	content, ok := candidates[0]["content"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected content object in candidate")
	}

	parts, ok := content["parts"].([]map[string]interface{})
	if !ok || len(parts) == 0 {
		t.Fatal("Expected parts array in content")
	}

	text, ok := parts[0]["text"].(string)
	if !ok {
		t.Fatal("Expected text string in part")
	}

	if text != "Hello from Gemini" {
		t.Errorf("Expected text 'Hello from Gemini', got '%s'", text)
	}
}

func TestCaptureBody(t *testing.T) {
	testData := "test body content"
	body := io.NopCloser(bytes.NewReader([]byte(testData)))

	captured, err := captureBody(body)
	if err != nil {
		t.Fatalf("captureBody() error = %v", err)
	}

	if string(captured) != testData {
		t.Errorf("Expected '%s', got '%s'", testData, string(captured))
	}
}

func TestCaptureBodyNil(t *testing.T) {
	captured, err := captureBody(nil)
	if err != nil {
		t.Fatalf("captureBody(nil) error = %v", err)
	}

	if captured != nil {
		t.Error("Expected nil for nil body")
	}
}
