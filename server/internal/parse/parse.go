// Package parse turns what an operator said into a draft warehouse operation.
package parse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"qoimeks/server/internal/store"
)

// Draft is the model's reading of one utterance. Codes are taken from the catalog;
// anything the operator did not say is left empty.
type Draft struct {
	Kind     string   `json:"kind"`
	SKUCodes []string `json:"sku_codes"`
	// LocationWords and ToLocationWords quote the part of the phrase a location
	// was read from. A location without such a quote was made up and is dropped.
	LocationWords   string   `json:"location_words"`
	LocationCode    string   `json:"location_code"`
	ToLocationWords string   `json:"to_location_words"`
	ToLocationCode  string   `json:"to_location_code"`
	Qty             *float64 `json:"qty"`
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
  One code when the phrase identifies a single SKU (usually by its article). When the phrase
  gives only a general name that several SKUs share ("кнопка выхода", "шығу түймесі"), return
  the three that fit best rather than picking one: the operator chooses. Empty when nothing in
  the catalog fits. Match on meaning across both languages. Operators often say the article
  instead of the name, letter by letter and with digits as words ("эс тэ е эл триста пятьдесят"
  for ST-EL350); a location they name can help choose between similar articles.
- location_words: the words of the phrase that name the place, copied exactly as they appear
  ("слева а три"), or an empty string when the operator names no place. Words that belong to
  the article or the quantity are not a place.
- location_code: the catalog location the goods were received into, issued from, counted at,
  or (for a transfer) moved out of. Location codes are a zone letter (L left/слева/сол жақ,
  R right/справа/оң жақ, D straight ahead/прямо/тура), a rack letter and a shelf number, said
  in any form: "слева а три" and "эл а три" are both LA3, "сол жақ эн екі" is LN2.
  Fill it only when the operator actually names a place. If they do not, or the place is not
  in the catalog, return an empty string: the app itself offers the SKU's usual location, so
  never copy the usual location from the catalog into this field.
- to_location_words, to_location_code: the same pair for the destination of a transfer.
  Empty strings otherwise.
- qty: the quantity as a number in the SKU's own unit, or null if not said.

Latin letters are spoken with their Russian names: а A, бэ B, цэ/си C, дэ D, е E, эф F, гэ/жэ G,
аш H, и I, ка K, эл L, эм M, эн N, о O, пэ P, ку Q, эр R, эс S, тэ T, у U, вэ V, икс X, зэт Z.

Use only codes that appear in the catalog below, copied exactly. Never guess a quantity.`

func systemPrompt(c store.Catalog) string {
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n<locations>\n")
	codes := make(map[int64]string, len(c.Locations))
	for _, l := range c.Locations {
		codes[l.ID] = l.Code
		fmt.Fprintf(&b, "%s\t%s\n", l.Code, l.Name)
	}
	b.WriteString("</locations>\n\nEach SKU line is: code, name, unit, usual location (may be empty).\n<skus>\n")
	for _, s := range c.SKUs {
		home := ""
		if s.LocationID != nil {
			home = codes[*s.LocationID]
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", s.Code, s.Name, s.Unit, home)
	}
	b.WriteString("</skus>")
	return b.String()
}

var draftSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"kind":              map[string]any{"type": "string", "enum": []string{"receipt", "issue", "transfer", "adjustment", "unknown"}},
		"sku_codes":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"location_words":    map[string]any{"type": "string"},
		"location_code":     map[string]any{"type": "string"},
		"to_location_words": map[string]any{"type": "string"},
		"to_location_code":  map[string]any{"type": "string"},
		"qty":               map[string]any{"type": []string{"number", "null"}},
	},
	"required":             []string{"kind", "sku_codes", "location_words", "location_code", "to_location_words", "to_location_code", "qty"},
	"additionalProperties": false,
}

// Claude parses phrases with the Anthropic API. The catalog travels in the system
// prompt, which is cached, so only the short phrase is paid for at full price.
type Claude struct {
	client anthropic.Client
	model  string
}

func NewClaude(apiKey, model string) *Claude {
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
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
