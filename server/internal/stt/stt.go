// Package stt converts recorded speech to text.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type Transcriber interface {
	// Transcribe returns the text spoken in audio. lang is an optional ISO 639-1
	// hint ("ru", "kk"); empty lets the service detect the language.
	Transcribe(ctx context.Context, audio []byte, filename, lang string) (string, error)
}

// OpenAICompatible talks to any service exposing POST {BaseURL}/audio/transcriptions
// in the OpenAI format (OpenAI itself, Groq, a self-hosted Whisper server, ...).
type OpenAICompatible struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func NewOpenAICompatible(baseURL, apiKey, model string) *OpenAICompatible {
	return &OpenAICompatible{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *OpenAICompatible) Transcribe(ctx context.Context, audio []byte, filename, lang string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	part.Write(audio)
	w.WriteField("model", t.Model)
	w.WriteField("response_format", "json")
	if lang != "" {
		w.WriteField("language", lang)
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if t.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.APIKey)
	}
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("speech service returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("speech service returned invalid JSON: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
