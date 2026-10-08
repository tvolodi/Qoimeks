package parse

import (
	"fmt"
	"sort"
	"strings"
)

// Provider is a known language-model service. Any other service with an
// OpenAI-style API works too: name it "custom" and give its address and model.
type Provider struct {
	BaseURL  string
	Model    string // default model, the cheapest one that is adequate for this task
	JSONMode string
	KeyVar   string // conventional environment variable holding the API key
}

var Providers = map[string]Provider{
	"anthropic": {Model: "claude-opus-5-5", KeyVar: "ANTHROPIC_API_KEY"},
	"openai":    {BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", JSONMode: JSONSchema, KeyVar: "OPENAI_API_KEY"},
	"deepseek":  {BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", JSONMode: JSONObject, KeyVar: "DEEPSEEK_API_KEY"},
	"minimax":   {BaseURL: "https://api.minimax.io/v1", Model: "MiniMax-M2.5", JSONMode: JSONPrompt, KeyVar: "MINIMAX_API_KEY"},
	"custom":    {JSONMode: JSONPrompt},
}

// ProviderNames lists the names accepted by New, for messages.
func ProviderNames() string {
	names := make([]string, 0, len(Providers))
	for n := range Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Settings choose and configure a provider. Empty fields fall back to the
// provider's defaults.
type Settings struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
	JSONMode string
}

// New builds the parser for the given settings and describes it for the log.
func New(s Settings) (Parser, string, error) {
	name := strings.ToLower(strings.TrimSpace(s.Provider))
	p, ok := Providers[name]
	if !ok {
		return nil, "", fmt.Errorf("unknown LLM provider %q (known: %s)", s.Provider, ProviderNames())
	}
	if s.APIKey == "" {
		return nil, "", fmt.Errorf("no API key for LLM provider %q", name)
	}
	if s.Model != "" {
		p.Model = s.Model
	}
	if s.BaseURL != "" {
		p.BaseURL = s.BaseURL
	}
	if s.JSONMode != "" {
		p.JSONMode = s.JSONMode
	}
	if p.Model == "" {
		return nil, "", fmt.Errorf("LLM provider %q needs a model name", name)
	}
	if name == "anthropic" {
		return NewClaude(s.APIKey, p.Model), "anthropic " + p.Model, nil
	}
	if p.BaseURL == "" {
		return nil, "", fmt.Errorf("LLM provider %q needs a base URL", name)
	}
	switch p.JSONMode {
	case JSONSchema, JSONObject, JSONPrompt:
	default:
		return nil, "", fmt.Errorf("unknown JSON mode %q (known: %s, %s, %s)", p.JSONMode, JSONSchema, JSONObject, JSONPrompt)
	}
	return NewOpenAICompatible(p.BaseURL, s.APIKey, p.Model, p.JSONMode), name + " " + p.Model, nil
}
