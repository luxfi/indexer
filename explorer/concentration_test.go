package explorer

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/explorer/explorer/testutil"
)

func TestGiniCoefficient(t *testing.T) {
	tests := []struct {
		name     string
		balances []float64
		wantMin  float64
		wantMax  float64
	}{
		{
			name:     "empty",
			balances: nil,
			wantMin:  0, wantMax: 0,
		},
		{
			name:     "single holder",
			balances: []float64{1000},
			wantMin:  0, wantMax: 0.01,
		},
		{
			name:     "perfect equality",
			balances: []float64{100, 100, 100, 100},
			wantMin:  0, wantMax: 0.01,
		},
		{
			name:     "high inequality",
			balances: []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 9991},
			wantMin:  0.8, wantMax: 1.0,
		},
		{
			name:     "moderate inequality",
			balances: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			wantMin:  0.2, wantMax: 0.4,
		},
		{
			name:     "zero sum",
			balances: []float64{0, 0, 0},
			wantMin:  0, wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := giniCoefficient(tt.balances)
			if g < tt.wantMin || g > tt.wantMax {
				t.Errorf("giniCoefficient() = %f, want [%f, %f]", g, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestQueryTokenDistribution(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	tokenAddrBytes := testutil.RandomAddress()

	// Insert holders with varying balances.
	values := []string{
		"50000000000000000000000", // 50% (whale)
		"30000000000000000000000", // 30% (whale)
		"10000000000000000000000", // 10% (large)
		"5000000000000000000000",  // 5% (large)
		"3000000000000000000000",  // 3% (large)
		"1000000000000000000000",  // 1% (medium)
		"500000000000000000000",   // 0.5% (medium)
		"300000000000000000000",   // 0.3% (medium)
		"100000000000000000000",   // 0.1% (medium)
		"100000000000000000000",   // 0.1% (medium)
	}

	for _, val := range values {
		tdb.InsertTokenBalance(t, testutil.TokenBalance{
			ChainID:              testutil.DefaultChainID,
			AddressHash:          testutil.RandomAddress(),
			TokenContractAddress: tokenAddrBytes,
			Value:                val,
			BlockNumber:          1,
			TokenType:            "ERC-20",
		})
	}

	ctx := context.Background()
	dist, err := queryTokenDistribution(ctx, tdb.DB, "address_current_token_balances", tokenAddrBytes)
	if err != nil {
		t.Fatalf("queryTokenDistribution: %v", err)
	}

	if dist.GiniCoefficient < 0.3 || dist.GiniCoefficient > 1.0 {
		t.Errorf("gini = %f, expected moderate-to-high inequality", dist.GiniCoefficient)
	}

	if len(dist.HolderTiers) == 0 {
		t.Error("expected holder tiers")
	}

	// Top 10 should be 100% (only 10 holders)
	if dist.Top10HoldersPct != "100.0" {
		t.Errorf("top_10_holders_pct = %s, want 100.0", dist.Top10HoldersPct)
	}
}

func TestQueryTokenDistribution_Empty(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	ctx := context.Background()
	dist, err := queryTokenDistribution(ctx, tdb.DB, "address_current_token_balances", "0xdeadbeef")
	if err != nil {
		t.Fatalf("queryTokenDistribution: %v", err)
	}

	if dist.GiniCoefficient != 0 {
		t.Errorf("gini = %f, want 0", dist.GiniCoefficient)
	}
	if dist.Top10HoldersPct != "0" {
		t.Errorf("top_10 = %s, want 0", dist.Top10HoldersPct)
	}
}

func TestTokenDistributionEndpoint(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	tokenAddr := testutil.RandomHexAddress()
	for _, val := range []string{"1000", "2000", "3000", "4000"} {
		tdb.InsertTokenBalance(t, testutil.TokenBalance{
			ChainID:              testutil.DefaultChainID,
			AddressHash:          testutil.RandomAddress(),
			TokenContractAddress: hexToBytes(tokenAddr),
			Value:                val,
			BlockNumber:          1,
			TokenType:            "ERC-20",
		})
	}

	srv, err := NewStandaloneServer(Config{
		IndexerDBPath: tdb.Path,
		ChainID:       testutil.DefaultChainID,
		ChainName:     "Test",
		CoinSymbol:    "TST",
	})
	if err != nil {
		t.Fatalf("NewStandaloneServer: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })

	resp, err := http.Get(ts.URL + "/v1/explorer/tokens/" + tokenAddr + "/distribution")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var dist distributionResponse
	if err := json.NewDecoder(resp.Body).Decode(&dist); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if dist.GiniCoefficient < 0 || dist.GiniCoefficient > 1 {
		t.Errorf("gini = %f, out of range", dist.GiniCoefficient)
	}
	if math.IsNaN(dist.GiniCoefficient) {
		t.Error("gini is NaN")
	}
}
