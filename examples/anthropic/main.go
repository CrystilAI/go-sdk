package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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
			ID:   "user-456",
			Name: "Jane Smith",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Register Anthropic client
	anthropicClient := client.RegisterAnthropic(
		context.Background(),
		option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
	)

	// Use Anthropic as normal - analytics tracked automatically
	message, err := anthropicClient.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("What is the capital of France?")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the first text block from the response
	for _, block := range message.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			fmt.Printf("Response: %s\n", textBlock.Text)
			break
		}
	}

	// Create a new transaction for the next request
	newTxClient := client.NewTransaction()

	anthropicClient2 := newTxClient.WrapAnthropicClient(
		context.Background(),
		os.Getenv("ANTHROPIC_API_KEY"),
	)

	message2, err := anthropicClient2.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("What is 2 + 2?")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the first text block from the response
	for _, block := range message2.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			fmt.Printf("Response: %s\n", textBlock.Text)
			break
		}
	}
}
