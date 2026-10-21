package main

import (
	"context"
	"fmt"
	"log"
	"os"

	crystil "github.com/CrystilAI/go-sdk"
	"google.golang.org/genai"
)

func main() {
	// Initialize Crystil client
	crystilClient, err := crystil.New(crystil.WithAPIKey(os.Getenv("CRYSTIL_API_KEY")))
	if err != nil {
		log.Fatal(err)
	}
	defer crystilClient.Close()

	// Get wrapped HTTP client for Google
	httpClient := crystilClient.HTTPClient(crystil.ProviderGoogle)

	ctx := context.Background()

	// Create Google GenAI client with your own configuration
	googleClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		HTTPClient: httpClient,
		Backend:    genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Set attribution to track costs by user. It rides in ctx, so it applies to
	// the calls made with ctx
	aliceJohnson := "Alice Johnson"
	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   "user-789",
		ParentName: &aliceJohnson,
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

	// Attribute the next request to a different user, through the same googleClient
	charlieBrown := "Charlie Brown"
	ctx, err = crystil.SetAttribution(ctx, &crystil.Attribution{
		ParentID:   "user-101",
		ParentName: &charlieBrown,
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
