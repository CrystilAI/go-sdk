package payloop

import (
	"context"
	"testing"
	"time"

	"github.com/PayloopAI/go-sdk/internal"
	"github.com/tmc/langchaingo/llms"
)

// mockLangChainCollector captures payloads for testing
type mockLangChainCollector struct {
	payloads []internal.Payload
}

func (m *mockLangChainCollector) SendAsync(ctx context.Context, p internal.Payload) {
	m.payloads = append(m.payloads, p)
}

func (m *mockLangChainCollector) Close() error {
	return nil
}

func TestNewHandler(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if handler.collector != realCollector {
		t.Error("Expected collector to be set")
	}

	if handler.cfg != cfg {
		t.Error("Expected config to be set")
	}

	if handler.txID != txID {
		t.Error("Expected txID to be set")
	}

	if handler.startTime == nil {
		t.Error("Expected startTime map to be initialized")
	}
}

func TestNewHandlerWithAttribution(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"
	attr := &Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
	}

	handler := newLangChainHandler(realCollector, cfg, txID, attr)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	if handler.attribution != attr {
		t.Error("Expected attribution to be set")
	}
}

func TestHandleLLMStart(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()
	prompts := []string{"test prompt"}

	// Record start time before calling
	before := time.Now()
	handler.HandleLLMStart(ctx, prompts)
	after := time.Now()

	// Verify start time was recorded
	startTime, ok := handler.startTime[txID]
	if !ok {
		t.Fatal("Expected start time to be recorded")
	}

	if startTime.Before(before) || startTime.After(after) {
		t.Error("Start time not in expected range")
	}
}

func TestHandleLLMEnd(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()

	// Set start time
	handler.HandleLLMStart(ctx, []string{"test"})

	// Simulate LLM end
	result := map[string]any{
		"response": "test response",
	}

	handler.HandleLLMEnd(ctx, result)

	// Verify payload was sent
	if len(mockCollector.payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(mockCollector.payloads))
	}

	payload := mockCollector.payloads[0]

	// Verify payload structure
	if payload.Meta.API.Key != "test-key" {
		t.Errorf("Expected API key 'test-key', got '%s'", payload.Meta.API.Key)
	}

	if payload.Meta.SDK.Version != "test-version" {
		t.Errorf("Expected version 'test-version', got '%s'", payload.Meta.SDK.Version)
	}

	if payload.Tx.UUID != txID {
		t.Errorf("Expected txID '%s', got '%s'", txID, payload.Tx.UUID)
	}

	if payload.Conversation.Client.Title != "langchain" {
		t.Errorf("Expected title 'langchain', got '%s'", payload.Conversation.Client.Title)
	}

	if payload.Meta.FNFG.Status != "succeeded" {
		t.Errorf("Expected status 'succeeded', got '%s'", payload.Meta.FNFG.Status)
	}

	// Verify start time was cleared
	if _, ok := handler.startTime[txID]; ok {
		t.Error("Expected start time to be cleared")
	}
}

func TestHandleLLMEndWithAttribution(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"
	attr := &Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
		Subsidiary: &AttributionEntity{
			ID:   "team-456",
			Name: "Engineering",
		},
	}

	handler := newLangChainHandler(realCollector, cfg, txID, attr)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()

	// Set start time
	handler.HandleLLMStart(ctx, []string{"test"})

	// Simulate LLM end
	result := map[string]any{
		"response": "test response",
	}

	handler.HandleLLMEnd(ctx, result)

	// Verify payload was sent with attribution
	if len(mockCollector.payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(mockCollector.payloads))
	}

	payload := mockCollector.payloads[0]

	// Verify attribution
	if payload.Attribution == nil {
		t.Fatal("Expected attribution to be set")
	}

	attrMap, ok := payload.Attribution.(map[string]interface{})
	if !ok {
		t.Fatal("Expected attribution to be a map")
	}

	parent, ok := attrMap["parent"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected parent in attribution")
	}

	if parent["id"] != "user-123" {
		t.Errorf("Expected parent ID 'user-123', got '%v'", parent["id"])
	}

	subsidiary, ok := attrMap["subsidiary"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected subsidiary in attribution")
	}

	if subsidiary["id"] != "team-456" {
		t.Errorf("Expected subsidiary ID 'team-456', got '%v'", subsidiary["id"])
	}
}

func TestHandleLLMGenerateContentEnd(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()

	// Set start time
	handler.HandleLLMGenerateContentStart(ctx, nil)

	// Simulate content generation end
	response := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "Generated content",
			},
		},
	}

	handler.HandleLLMGenerateContentEnd(ctx, response)

	// Verify payload was sent
	if len(mockCollector.payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(mockCollector.payloads))
	}

	payload := mockCollector.payloads[0]

	// Verify payload structure
	if payload.Meta.FNFG.Status != "succeeded" {
		t.Errorf("Expected status 'succeeded', got '%s'", payload.Meta.FNFG.Status)
	}

	// Verify response contains choices
	choices, ok := payload.Conversation.Response["choices"].([]map[string]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("Expected choices in response")
	}

	message, ok := choices[0]["message"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected message in choice")
	}

	content, ok := message["content"].(string)
	if !ok || content != "Generated content" {
		t.Errorf("Expected content 'Generated content', got '%v'", content)
	}
}

func TestHandleLLMError(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()

	// Set start time
	handler.HandleLLMStart(ctx, []string{"test"})

	// Simulate LLM error
	testErr := &testLangChainError{msg: "test error"}
	handler.HandleLLMError(ctx, testErr)

	// Verify payload was sent
	if len(mockCollector.payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(mockCollector.payloads))
	}

	payload := mockCollector.payloads[0]

	// Verify error status
	if payload.Meta.FNFG.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", payload.Meta.FNFG.Status)
	}

	if payload.Meta.FNFG.Exc == nil {
		t.Fatal("Expected error message to be set")
	}

	if *payload.Meta.FNFG.Exc != "test error" {
		t.Errorf("Expected error 'test error', got '%s'", *payload.Meta.FNFG.Exc)
	}

	// Verify start time was cleared
	if _, ok := handler.startTime[txID]; ok {
		t.Error("Expected start time to be cleared")
	}
}

func TestHandleLLMEndWithoutStart(t *testing.T) {
	mockCollector := &mockLangChainCollector{}
	cfg := &config{
		apiKey:  "test-key",
		version: "test-version",
	}
	realCollector := newCollector(cfg)
	defer realCollector.Close()

	txID := "test-tx-id"

	handler := newLangChainHandler(realCollector, cfg, txID, nil)
	// Override sendFn to capture payloads in mock
	handler.sendFn = mockCollector.SendAsync

	ctx := context.Background()

	// Simulate LLM end without calling start
	result := map[string]any{
		"response": "test response",
	}

	handler.HandleLLMEnd(ctx, result)

	// Should still send payload with fallback time
	if len(mockCollector.payloads) != 1 {
		t.Fatalf("Expected 1 payload, got %d", len(mockCollector.payloads))
	}

	payload := mockCollector.payloads[0]

	// Verify times are set (even if start wasn't called)
	if payload.Time.Start.IsZero() {
		t.Error("Expected start time to be set")
	}

	if payload.Time.End.IsZero() {
		t.Error("Expected end time to be set")
	}
}

// testLangChainError is a simple error type for testing
type testLangChainError struct {
	msg string
}

func (e *testLangChainError) Error() string {
	return e.msg
}
