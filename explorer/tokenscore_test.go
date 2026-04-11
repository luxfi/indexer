package explorer

import "testing"

func TestComputeTokenScore(t *testing.T) {
	tests := []struct {
		name  string
		token map[string]any
		want  int
	}{
		{
			name:  "empty token scores zero",
			token: map[string]any{},
			want:  0,
		},
		{
			name: "verified contract only",
			token: map[string]any{
				"is_verified": true,
			},
			want: 20,
		},
		{
			name: "holder_count > 10",
			token: map[string]any{
				"holder_count": int64(15),
			},
			want: 20,
		},
		{
			name: "holder_count > 100 gets both tiers",
			token: map[string]any{
				"holder_count": int64(200),
			},
			want: 35, // 20 (>10) + 15 (>100)
		},
		{
			name: "holder_count > 1000 gets all tiers",
			token: map[string]any{
				"holder_count": int64(5000),
			},
			want: 45, // 20 + 15 + 10
		},
		{
			name: "icon_url adds 15",
			token: map[string]any{
				"icon_url": "https://example.com/icon.png",
			},
			want: 15,
		},
		{
			name: "fiat_value adds 10",
			token: map[string]any{
				"fiat_value": "1.23",
			},
			want: 10,
		},
		{
			name: "total_supply > 0 adds 10",
			token: map[string]any{
				"total_supply": "1000000",
			},
			want: 10,
		},
		{
			name: "max score scenario",
			token: map[string]any{
				"is_verified":  true,
				"holder_count": int64(5000),
				"icon_url":     "https://icon.png",
				"fiat_value":   "2.50",
				"total_supply": "1000000000000000000000000",
			},
			want: 100, // 20+45+15+10+10=100
		},
		{
			name: "capped at 100",
			token: map[string]any{
				"is_verified":  int64(1),
				"holder_count": int64(5000),
				"icon_url":     "https://icon.png",
				"fiat_value":   "2.50",
				"total_supply": "99999999",
			},
			want: 100,
		},
		{
			name:  "nil total_supply scores zero for that field",
			token: map[string]any{"total_supply": nil},
			want:  0,
		},
		{
			name:  "zero total_supply scores zero for that field",
			token: map[string]any{"total_supply": "0"},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeTokenScore(tt.token)
			if got != tt.want {
				t.Errorf("computeTokenScore() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToBool(t *testing.T) {
	if !toBool(true) {
		t.Error("bool true")
	}
	if toBool(false) {
		t.Error("bool false")
	}
	if !toBool(int64(1)) {
		t.Error("int64 1")
	}
	if toBool(int64(0)) {
		t.Error("int64 0")
	}
	if !toBool(float64(1)) {
		t.Error("float64 1")
	}
	if toBool("") {
		t.Error("string empty")
	}
}

func TestToInt(t *testing.T) {
	if toInt(42) != 42 {
		t.Error("int")
	}
	if toInt(int64(99)) != 99 {
		t.Error("int64")
	}
	if toInt(float64(7.9)) != 7 {
		t.Error("float64")
	}
	if toInt("123") != 123 {
		t.Error("string")
	}
	if toInt(nil) != 0 {
		t.Error("nil")
	}
}
