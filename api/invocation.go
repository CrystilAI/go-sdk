package api

import (
	"context"
	"fmt"
	"time"
)

// InvocationClient manages invocation operations.
type InvocationClient struct {
	workflowUUID string
	api          *Client
	attribution  map[string]interface{}
}

// Attribution sets attribution filter for summary queries.
// Returns a new client with attribution set.
func (i *InvocationClient) Attribution(parentID, parentName string, subsidiaryID, subsidiaryName *string) *InvocationClient {
	cp := *i
	cp.attribution = map[string]interface{}{
		"parent": map[string]interface{}{
			"id":   parentID,
			"name": parentName,
		},
	}

	if subsidiaryID != nil {
		cp.attribution["subsidiary"] = map[string]interface{}{
			"id":   *subsidiaryID,
			"name": subsidiaryName,
		}
	}

	return &cp
}

// SummaryOptions configures a summary request.
type SummaryOptions struct {
	DateStart time.Time
	DateEnd   *time.Time
}

// Summary retrieves invocation summary for a workflow.
func (i *InvocationClient) Summary(ctx context.Context, opts SummaryOptions) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"date": map[string]interface{}{
			"start": opts.DateStart.Format(time.RFC3339),
		},
	}

	if opts.DateEnd != nil {
		body["date"].(map[string]interface{})["end"] = opts.DateEnd.Format(time.RFC3339)
	}

	if i.attribution != nil {
		body["attribution"] = i.attribution
	}

	var result map[string]interface{}
	path := fmt.Sprintf("workflow/%s/invocation/summary", i.workflowUUID)
	if err := i.api.post(ctx, path, body, &result); err != nil {
		return nil, fmt.Errorf("get invocation summary: %w", err)
	}

	return result, nil
}
