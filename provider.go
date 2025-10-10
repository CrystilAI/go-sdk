package payloop

// Provider represents a supported LLM provider.
type Provider string

const (
	// ProviderOpenAI represents the OpenAI API provider.
	ProviderOpenAI Provider = "openai"

	// ProviderAnthropic represents the Anthropic API provider.
	ProviderAnthropic Provider = "anthropic"

	// ProviderGoogle represents the Google GenAI API provider.
	ProviderGoogle Provider = "google"
)
