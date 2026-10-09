package insights

import "strings"

// Price is a model's list price in USD per million tokens.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheWrite float64 `json:"cache_write"` // 5-minute TTL write (1.25x input)
	CacheRead  float64 `json:"cache_read"`
}

// cacheWrite1h is the 1-hour TTL write multiplier on the input price.
const cacheWrite1h = 2.0

// PriceRow maps a model-id substring to a tier and a price.
type PriceRow struct {
	Match string // lower-case substring of the transcript's message.model
	Tier  string // opus | sonnet | haiku | fable
	Price Price
}

// Prices is the pricing table, most specific match first. Update it when
// Anthropic changes list prices or ships a model; the first row whose Match
// is a substring of the model id wins. Source: Anthropic first-party API
// list prices as of 2026-10. Haiku 5.5's long-prompt tier (prompts over
// 100K tokens at $0.50/$2.50) is ignored: the table uses the short tier.
var Prices = []PriceRow{
	{"fable-5-1", "fable", Price{10, 50, 12.5, 0.25}},
	{"mythos-5-1", "fable", Price{10, 50, 12.5, 0.25}},
	{"fable", "fable", Price{10, 50, 12.5, 1.00}},
	{"mythos", "fable", Price{10, 50, 12.5, 1.00}},
	{"opus-5-5", "opus", Price{4, 20, 5, 0.20}},
	{"opus", "opus", Price{5, 25, 6.25, 0.50}},      // opus 5, 4.8, 4.7, 4.6
	{"sonnet-5", "sonnet", Price{2, 10, 2.5, 0.20}}, // sonnet 5.5 and 5
	{"sonnet", "sonnet", Price{3, 15, 3.75, 0.30}},  // sonnet 4.x
	{"haiku-5", "haiku", Price{0.10, 0.50, 0.125, 0.01}},
	{"haiku", "haiku", Price{1, 5, 1.25, 0.10}}, // haiku 4.5
}

// LookupPrice returns the row for a model id, or false for an unknown or
// synthetic model (those are not costed).
func LookupPrice(model string) (PriceRow, bool) {
	m := strings.ToLower(model)
	for _, r := range Prices {
		if strings.Contains(m, r.Match) {
			return r, true
		}
	}
	return PriceRow{}, false
}

// TierOf returns opus | sonnet | haiku | fable, or "" when unknown.
func TierOf(model string) string {
	r, ok := LookupPrice(model)
	if !ok {
		return ""
	}
	return r.Tier
}

// Usage is the token usage of one assistant message.
type Usage struct {
	Input       int64 `json:"input_tokens"`
	Output      int64 `json:"output_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
	CacheWrite  int64 `json:"cache_creation_input_tokens"`
	CacheDetail *struct {
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation,omitempty"`
}

// Context is the size of the context window this message saw.
func (u Usage) Context() int64 { return u.Input + u.CacheRead + u.CacheWrite }

// CostUSD estimates what u cost on model. Cache writes with a 1-hour TTL
// are priced at 2x input, the rest at the 5-minute rate.
func CostUSD(model string, u Usage) float64 {
	r, ok := LookupPrice(model)
	if !ok {
		return 0
	}
	p := r.Price
	w1h := int64(0)
	if u.CacheDetail != nil {
		w1h = u.CacheDetail.Write1h
		if w1h > u.CacheWrite {
			w1h = u.CacheWrite
		}
	}
	usd := float64(u.Input)*p.Input +
		float64(u.Output)*p.Output +
		float64(u.CacheRead)*p.CacheRead +
		float64(u.CacheWrite-w1h)*p.CacheWrite +
		float64(w1h)*p.Input*cacheWrite1h
	return usd / 1e6
}

// tierOutputPrice is the output price per tier used for the routing
// savings estimate (current generation: Opus 5.5, Sonnet 5.5, Haiku 5.5).
var tierOutputPrice = map[string]float64{"opus": 20, "sonnet": 10, "haiku": 0.5, "fable": 50}

// TierRatio is price(pick)/price(parent) by output price, or 1 when either
// tier is unknown.
func TierRatio(pick, parent string) float64 {
	a, b := tierOutputPrice[pick], tierOutputPrice[parent]
	if a == 0 || b == 0 {
		return 1
	}
	return a / b
}
