package payloop

import (
	"context"
	"os"
	"testing"
	"time"
)

func init() {
	// Enable test mode to prevent actual network calls
	os.Setenv("PAYLOOP_TEST_MODE", "1")
}

func TestNewLangChainHandlerCreation(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
}

func TestNewLangChainHandlerWithAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = client.SetAttribution(&Attribution{
		Parent: AttributionEntity{
			ID:   "user-123",
			Name: "Test User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := client.NewLangChainHandler()
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
}

func TestHandleLLMStart(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()

	ctx := context.Background()
	prompts := []string{"test prompt"}

	// Record start time before calling
	before := time.Now()
	handler.HandleLLMStart(ctx, prompts)
	after := time.Now()

	// Verify start time was recorded (access internal field for testing)
	h := handler.(*langChainHandler)
	startTime, ok := h.startTime[client.txID]
	if !ok {
		t.Fatal("Expected start time to be recorded")
	}

	if startTime.Before(before) || startTime.After(after) {
		t.Error("Start time not in expected range")
	}
}

func TestHandleLLMEnd(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()
	h := handler.(*langChainHandler)

	ctx := context.Background()

	// Set start time
	h.HandleLLMStart(ctx, []string{"test"})

	// Simulate LLM end
	result := map[string]any{
		"response": "test response",
	}

	h.HandleLLMEnd(ctx, result)

	// Verify start time was cleared
	if _, ok := h.startTime[client.txID]; ok {
		t.Error("Expected start time to be cleared after HandleLLMEnd")
	}
}

func TestHandleLLMError(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()

	ctx := context.Background()

	// Set start time
	handler.HandleLLMStart(ctx, []string{"test"})

	// Simulate LLM error
	testErr := &testLangChainError{msg: "test error"}
	handler.HandleLLMError(ctx, testErr)

	// Verify start time was cleared
	h := handler.(*langChainHandler)
	if _, ok := h.startTime[client.txID]; ok {
		t.Error("Expected start time to be cleared after HandleLLMError")
	}
}

func TestHandlerMutableAttribution(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := client.NewLangChainHandler()
	h := handler.(*langChainHandler)

	ctx := context.Background()

	// First request without attribution
	h.HandleLLMStart(ctx, []string{"test1"})
	h.HandleLLMEnd(ctx, map[string]any{"response": "test1"})

	// Update attribution on the client
	err = client.SetAttribution(&Attribution{
		Parent: AttributionEntity{
			ID:   "user-456",
			Name: "Updated User",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second request should use new attribution (same handler)
	// Verify handler still works after attribution update
	h.HandleLLMStart(ctx, []string{"test2"})
	h.HandleLLMEnd(ctx, map[string]any{"response": "test2"})

	// Verify handler is still functional
	if h.client.attribution == nil {
		t.Fatal("Expected attribution to be set on client")
	}

	if h.client.attribution.Parent.ID != "user-456" {
		t.Errorf("Expected attribution ID 'user-456', got '%s'", h.client.attribution.Parent.ID)
	}
}

// testLangChainError is a simple error type for testing
type testLangChainError struct {
	msg string
}

func (e *testLangChainError) Error() string {
	return e.msg
}
