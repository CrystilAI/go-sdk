// Package payloop provides cost visibility for AI agent deployments.
//
// Payloop tracks costs across OpenAI, Anthropic, Google, and other LLM
// providers with a single line of code. It captures request/response data,
// timing, and attribution for real-time cost analysis.
//
// Basic usage:
//
//	client, err := payloop.New(payloop.WithAPIKey("pl_..."))
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
//
//	openaiClient, err := client.RegisterOpenAI(ctx, openai.NewClient("sk-..."))
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Use openaiClient as normal - analytics sent automatically
//	resp, err := openaiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
//		Model: openai.GPT4,
//		Messages: []openai.ChatCompletionMessage{
//			{Role: "user", Content: "Hello!"},
//		},
//	})
package payloop
