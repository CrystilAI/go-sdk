package api

import (
	"context"
	"fmt"
)

// Workflow represents a Payloop workflow.
type Workflow struct {
	UUID  string `json:"uuid"`
	Label string `json:"label"`
}

// WorkflowsClient manages multiple workflows.
type WorkflowsClient struct {
	api *Client
}

// NewWorkflowsClient creates a new workflows client.
func NewWorkflowsClient(api *Client) *WorkflowsClient {
	return &WorkflowsClient{api: api}
}

// List returns all workflows.
func (w *WorkflowsClient) List(ctx context.Context) ([]Workflow, error) {
	var result []Workflow
	if err := w.api.get(ctx, "workflows", &result); err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	return result, nil
}

// WorkflowClient manages a single workflow.
type WorkflowClient struct {
	uuid string
	api  *Client
}

// NewWorkflowClient creates a new workflow client.
func NewWorkflowClient(uuid string, api *Client) *WorkflowClient {
	return &WorkflowClient{
		uuid: uuid,
		api:  api,
	}
}

// Update updates a workflow's label.
func (w *WorkflowClient) Update(ctx context.Context, label string) error {
	body := map[string]string{"label": label}
	if err := w.api.patch(ctx, "workflow/"+w.uuid, body, nil); err != nil {
		return fmt.Errorf("update workflow: %w", err)
	}
	return nil
}

// Invocation returns a client for invocation operations.
func (w *WorkflowClient) Invocation() *InvocationClient {
	return &InvocationClient{
		workflowUUID: w.uuid,
		api:          w.api,
	}
}
