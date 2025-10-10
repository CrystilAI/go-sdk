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
	client, err := payloop.New(payloop.WithAPIKey(os.Getenv("PAYLOOP_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Set attribution to track costs by user
	client, err = client.SetAttribution(payloop.Attribution{
		Parent: payloop.AttributionEntity{
			ID:   "user-789",
			Name: "Alice Johnson",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Register Google GenAI client with Payloop tracking
	googleClient, err := client.RegisterGoogle(
		ctx,
		&genai.ClientConfig{
			APIKey:  os.Getenv("GOOGLE_API_KEY"),
			Backend: genai.BackendGeminiAPI,
		},
	)
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

	// Create a new transaction for the next request
	newTxClient := client.NewTransaction()

	googleClient2, err := newTxClient.WrapGoogleClient(
		ctx,
		os.Getenv("GOOGLE_API_KEY"),
	)
	if err != nil {
		log.Fatal(err)
	}

	resp2, err := googleClient2.Models.GenerateContent(
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
