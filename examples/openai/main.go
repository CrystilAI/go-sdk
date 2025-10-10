package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/PayloopAI/go-sdk"
	"github.com/openai/openai-go/v3"
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
			ID:   "user-123",
			Name: "John Doe",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Register OpenAI client
	openaiClient := client.WrapOpenAIClient(context.Background(), os.Getenv("OPENAI_API_KEY"))

	// Use OpenAI as normal - analytics tracked automatically
	resp, err := openaiClient.Chat.Completions.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Model: openai.ChatModelGPT4o,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("What is the capital of France?"),
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", resp.Choices[0].Message.Content)

	// Create a new transaction for the next request
	newTxClient := client.NewTransaction()

	openaiClient2 := newTxClient.WrapOpenAIClient(context.Background(), os.Getenv("OPENAI_API_KEY"))

	resp2, err := openaiClient2.Chat.Completions.New(
		context.Background(),
		openai.ChatCompletionNewParams{
			Model: openai.ChatModelGPT4o,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("What is 2 + 2?"),
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Response: %s\n", resp2.Choices[0].Message.Content)
}
