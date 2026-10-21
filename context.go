package crystil

import (
	"context"

	"github.com/google/uuid"
)

type transactionKey struct{}

type attributionKey struct{}

// NewTransaction returns a copy of ctx that starts a new transaction.
// Provider calls made with the returned context, or a context made from it, are
// recorded under that transaction instead of the client's.
//
// Example:
//
//	ctx = crystil.NewTransaction(ctx)
//	resp, err := openaiClient.Chat.Completions.New(ctx, params)
func NewTransaction(ctx context.Context) context.Context {
	return context.WithValue(ctx, transactionKey{}, uuid.New().String())
}

// SetAttribution returns a copy of ctx whose provider calls are attributed to
// attr, in the same transaction as ctx. A nil attr removes the attribution ctx
// carries. It keeps attr's values as they are now: changing attr afterwards
// changes no context already returned.
//
// Example:
//
//	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{ParentID: "customer_123"})
//	resp, err := openaiClient.Chat.Completions.New(ctx, params)
func SetAttribution(ctx context.Context, attr *Attribution) (context.Context, error) {
	var payload map[string]interface{}
	if attr != nil {
		if err := attr.validate(); err != nil {
			return ctx, err
		}
		payload = attr.payload()
	}
	return context.WithValue(ctx, attributionKey{}, payload), nil
}

// transaction returns the ID of the transaction ctx carries, or the client's.
func (c *Client) transaction(ctx context.Context) string {
	if id, ok := ctx.Value(transactionKey{}).(string); ok {
		return id
	}
	return c.txID
}

// attributionPayload returns the attribution ctx carries, as the payload
// records it.
func attributionPayload(ctx context.Context) interface{} {
	if payload, _ := ctx.Value(attributionKey{}).(map[string]interface{}); payload != nil {
		return payload
	}
	return nil
}
