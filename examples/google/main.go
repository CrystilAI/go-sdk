package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"google.golang.org/genai"
)

func main() {
	// Initialize Payloop client
	payloopClient, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer payloopClient.Close()

	// Get wrapped HTTP client for Google
	httpClient := payloopClient.HTTPClient(payloop.ProviderGoogle)

	ctx := context.Background()

	// Create Google GenAI client with your own configuration
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		HTTPClient: httpClient,
		APIKey:     os.Getenv("GOOGLE_API_KEY"),
		Backend:    genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Set attribution to track costs by user (affects all future requests)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-789",
			Name: "Alice Johnson",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Use Google GenAI as normal - analytics tracked automatically
	resp, err := googleClient.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{{Parts: []*genai.Part{{Text: "What is the meaning of life?"}}}},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Print the response
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				fmt.Printf("Response: %v\n", part)
			}
		}
	}

	// Update attribution for a different user (affects same googleClient)
	err = payloopClient.SetAttribution(&payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-101",
			Name: "Charlie Brown",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// This request will be attributed to Charlie Brown
	resp2, err := googleClient.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{{Parts: []*genai.Part{{Text: "Tell me a short joke"}}}},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Print the response
	for _, cand := range resp2.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				fmt.Printf("Response: %v\n", part)
			}
		}
	}
}
