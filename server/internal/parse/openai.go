package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"qoimeks/server/internal/store"
)

// How a service can be told to answer in JSON. Services differ, so the prompt
// always describes the object too and the answer is always parsed leniently.
const (
	JSONSchema = "schema" // response_format json_schema: the answer is guaranteed to fit
	JSONObject = "object" // response_format json_object: valid JSON, shape not guaranteed
	JSONPrompt = "prompt" // no response_format: only the prompt asks for JSON
)

const jsonInstructions = `

Answer with one JSON object and nothing else, with exactly these keys:
{"kind": "receipt" | "issue" | "transfer" | "adjustment" | "unknown",
 "sku_codes": [string, ...],
 "location_words": string,
 "location_code": string,
 "to_location_words": string,
 "to_location_code": string,
 "qty": number or null}`

// OpenAICompatible parses phrases with any service that speaks the OpenAI
// chat-completions format: OpenAI itself, DeepSeek, MiniMax and many others.
type OpenAICompatible struct {
	BaseURL  string
	APIKey   string
	Model    string
	JSONMode string
	HTTP     *http.Client
}

func NewOpenAICompatible(baseURL, apiKey, model, jsonMode string) *OpenAICompatible {
	return &OpenAICompatible{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		APIKey:   apiKey,
		Model:    model,
		JSONMode: jsonMode,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAICompatible) Parse(ctx context.Context, catalog store.Catalog, text string) (Draft, error) {
	var d Draft
	// The catalog goes first and never changes between phrases, so services that
	// cache a repeated prompt prefix (OpenAI and DeepSeek do) charge little for it.
	request := map[string]any{
		"model": p.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt(catalog) + jsonInstructions},
			{"role": "user", "content": text},
		},
	}
	switch p.JSONMode {
	case JSONSchema:
		request["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "draft", "strict": true, "schema": draftSchema},
		}
	case JSONObject:
		request["response_format"] = map[string]any{"type": "json_object"}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return d, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return d, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return d, err
	}
	if resp.StatusCode != http.StatusOK {
		return d, fmt.Errorf("%s returned %s: %s", p.BaseURL, resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return d, fmt.Errorf("%s returned invalid JSON: %w", p.BaseURL, err)
	}
	if len(out.Choices) == 0 {
		return d, errors.New("model returned no answer")
	}
	if out.Choices[0].Message.Refusal != "" {
		return d, ErrRefused
	}
	object, err := extractJSON(out.Choices[0].Message.Content)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(object), &d); err != nil {
		return d, fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return d, nil
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

// extractJSON finds the JSON object in a model's answer, which may be wrapped in
// a code fence, preceded by visible reasoning, or followed by a comment.
func extractJSON(answer string) (string, error) {
	answer = thinkBlock.ReplaceAllString(answer, "")
	start := strings.Index(answer, "{")
	end := strings.LastIndex(answer, "}")
	if start < 0 || end < start {
		return "", errors.New("model answer contains no JSON object")
	}
	return answer[start : end+1], nil
}
