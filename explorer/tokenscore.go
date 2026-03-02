package explorer

import (
	"strconv"
)

// computeTokenScore returns a 0-100 trust score for a token based on on-chain signals.
func computeTokenScore(token map[string]any) int {
	score := 0

	// +20 if verified contract (is_verified present and true)
	if v, ok := token["is_verified"]; ok && toBool(v) {
		score += 20
	}

	holders := toInt(token["holder_count"])
	if holders > 10 {
		score += 20
	}
	if holders > 100 {
		score += 15
	}
	if holders > 1000 {
		score += 10
	}

	// +15 if has icon_url
	if v := token["icon_url"]; v != nil && v != "" {
		score += 15
	}

	// +10 if has exchange_rate / fiat_value (listed on CoinGecko/CMC)
	if v := token["fiat_value"]; v != nil && v != "" {
		score += 10
	}

	// +10 if total_supply > 0
	if supply := token["total_supply"]; supply != nil {
		s := fmtNum(supply)
		if s != "" && s != "0" && s != "nil" {
			score += 10
		}
	}

	if score > 100 {
		score = 100
	}
	return score
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int64:
		return b != 0
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}
