// Package parse turns what an operator said into a draft warehouse operation.
package parse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"qoimeks/server/internal/store"
)

// Draft is the model's reading of one utterance. Codes are taken from the catalog;
// anything the operator did not say is left empty.
type Draft struct {
	Kind           string   `json:"kind"`
	SKUCodes       []string `json:"sku_codes"`
	LocationCode   string   `json:"location_code"`
	ToLocationCode string   `json:"to_location_code"`
	Qty            *float64 `json:"qty"`
}

type Parser interface {
	Parse(ctx context.Context, catalog store.Catalog, text string) (Draft, error)
}

var ErrRefused = errors.New("the model declined to parse this phrase")

const instructions = `You turn one spoken phrase from a warehouse operator into a draft stock operation.
The phrase is a speech-recognition transcript in Russian, Kazakh, or a mix of both, so expect
misheard words, numbers written as words, and codes spelled out loosely ("а один" for A-01).

Fill the fields from the phrase only:
- kind: "receipt" (goods arrive: приход, принял, получил, кіріс, қабылдадым),
  "issue" (goods leave: расход, отпустил, выдал, списал, шығыс, бердім),
  "transfer" (moved between two locations: переместил, перенёс, ауыстырдым),
  "adjustment" (a stock count, the quantity is what is physically there: инвентаризация,
  пересчитал, по факту, түгендеу, санадым), or "unknown" if the phrase does not say.
- sku_codes: codes of the catalog SKUs the operator most likely meant, best match first.
  One code when you are confident, up to three when several fit about equally well,
  empty when nothing in the catalog fits. Match on meaning across both languages.
- location_code: the catalog location the goods were received into, issued from, counted at,
  or (for a transfer) moved out of. Empty string if not said or not in the catalog.
- to_location_code: for a transfer, the destination location. Empty string otherwise.
- qty: the quantity as a number in the SKU's own unit, or null if not said.

Use only codes that appear in the catalog below, copied exactly. Never guess a quantity.`

func systemPrompt(c store.Catalog) string {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n<locations>\n")
	for _, l := range c.Locations {
		fmt.Fprintf(&b, "%s\t%s\n", l.Code, l.Name)
	}
	b.WriteString("</locations>\n\n<skus>\n")
	for _, s := range c.SKUs {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", s.Code, s.Name, s.Unit)
	}
	b.WriteString("</skus>")
	return b.String()
}

var draftSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"kind":             map[string]any{"type": "string", "enum": []string{"receipt", "issue", "transfer", "adjustment", "unknown"}},
		"sku_codes":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"location_code":    map[string]any{"type": "string"},
		"to_location_code": map[string]any{"type": "string"},
		"qty":              map[string]any{"type": []string{"number", "null"}},
	},
	"required":             []string{"kind", "sku_codes", "location_code", "to_location_code", "qty"},
	"additionalProperties": false,
}

// Claude parses phrases with the Anthropic API. The catalog travels in the system
// prompt, which is cached, so only the short phrase is paid for at full price.
type Claude struct {
	client anthropic.Client
	model  string
}

// NewClaude reads credentials from the environment (ANTHROPIC_API_KEY).
func NewClaude(model string) *Claude {
	return &Claude{client: anthropic.NewClient(), model: model}
}

func (p *Claude) Parse(ctx context.Context, catalog store.Catalog, text string) (Draft, error) {
	var d Draft
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: 16000,
		System: []anthropic.TextBlockParam{{
			Text:         systemPrompt(catalog),
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: draftSchema},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
		},
	})
	if err != nil {
		return d, err
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return d, ErrRefused
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return d, errors.New("model output was cut off")
	}
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			if err := json.Unmarshal([]byte(t.Text), &d); err != nil {
				return d, fmt.Errorf("model returned invalid JSON: %w", err)
			}
			return d, nil
		}
	}
	return d, errors.New("model returned no text")
}
