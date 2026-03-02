package explorer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"sort"
	"time"

	"github.com/hanzoai/base/core"
)

// giniCoefficient computes the Gini coefficient for a sorted slice of balances.
// Returns 0 for empty or zero-sum inputs. Values must be non-negative.
func giniCoefficient(balances []float64) float64 {
	n := float64(len(balances))
	if n == 0 {
		return 0
	}
	sort.Float64s(balances)
	sum := 0.0
	for _, b := range balances {
		sum += b
	}
	if sum == 0 {
		return 0
	}
	weightedSum := 0.0
	for i, b := range balances {
		weightedSum += float64(i+1) * b
	}
	g := (2*weightedSum)/(n*sum) - (n+1)/n
	if g < 0 {
		g = 0
	}
	return g
}

type holderTier struct {
	Tier      string `json:"tier"`
	Count     int    `json:"count"`
	PctSupply string `json:"pct_supply"`
}

type distributionResponse struct {
	GiniCoefficient  float64      `json:"gini_coefficient"`
	Top10HoldersPct  string       `json:"top_10_holders_pct"`
	Top100HoldersPct string       `json:"top_100_holders_pct"`
	HolderTiers      []holderTier `json:"holder_tiers"`
}

// queryTokenDistribution loads balances from the DB and computes distribution stats.
func queryTokenDistribution(ctx context.Context, db *sql.DB, table string, tokenAddr any) (*distributionResponse, error) {
	rows, err := db.QueryContext(ctx,
		fmt.Sprintf("SELECT value FROM %s WHERE token_contract_address_hash = ? AND value IS NOT NULL ORDER BY CAST(value AS REAL) DESC LIMIT 10000", table),
		tokenAddr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []float64
	totalSupply := new(big.Float)

	for rows.Next() {
		var valStr string
		if err := rows.Scan(&valStr); err != nil {
			continue
		}
		bf, _, err := new(big.Float).Parse(valStr, 10)
		if err != nil {
			continue
		}
		f, _ := bf.Float64()
		if f <= 0 {
			continue
		}
		balances = append(balances, f)
		totalSupply.Add(totalSupply, bf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(balances) == 0 {
		return &distributionResponse{
			GiniCoefficient:  0,
			Top10HoldersPct:  "0",
			Top100HoldersPct: "0",
			HolderTiers:      []holderTier{},
		}, nil
	}

	// balances already sorted DESC from SQL
	totalF, _ := totalSupply.Float64()

	// Top 10 and top 100 percentages.
	top10Sum := 0.0
	top100Sum := 0.0
	for i, b := range balances {
		if i < 10 {
			top10Sum += b
		}
		if i < 100 {
			top100Sum += b
		}
	}

	// Tiers by % of supply: whale >1%, large 0.1-1%, medium 0.01-0.1%, small <0.01%
	type tierAcc struct {
		count int
		sum   float64
	}
	tiers := [4]tierAcc{} // 0=whale, 1=large, 2=medium, 3=small
	for _, b := range balances {
		pct := (b / totalF) * 100
		switch {
		case pct > 1:
			tiers[0].count++
			tiers[0].sum += b
		case pct > 0.1:
			tiers[1].count++
			tiers[1].sum += b
		case pct > 0.01:
			tiers[2].count++
			tiers[2].sum += b
		default:
			tiers[3].count++
			tiers[3].sum += b
		}
	}

	tierNames := [4]string{"whale (>1%)", "large (0.1-1%)", "medium (0.01-0.1%)", "small (<0.01%)"}
	holderTiers := make([]holderTier, 0, 4)
	for i, t := range tiers {
		if t.count == 0 {
			continue
		}
		holderTiers = append(holderTiers, holderTier{
			Tier:      tierNames[i],
			Count:     t.count,
			PctSupply: fmt.Sprintf("%.1f", (t.sum/totalF)*100),
		})
	}

	// Sort ascending for Gini calculation.
	sorted := make([]float64, len(balances))
	copy(sorted, balances)

	gini := giniCoefficient(sorted)
	gini = math.Round(gini*100) / 100

	return &distributionResponse{
		GiniCoefficient:  gini,
		Top10HoldersPct:  fmt.Sprintf("%.1f", (top10Sum/totalF)*100),
		Top100HoldersPct: fmt.Sprintf("%.1f", (top100Sum/totalF)*100),
		HolderTiers:      holderTiers,
	}, nil
}

// handleTokenDistribution serves GET /v1/explorer/tokens/{address_hash}/distribution (plugin mode).
func (p *plugin) handleTokenDistribution(e *core.RequestEvent) error {
	ctx, cancel := context.WithTimeout(e.Request.Context(), 10*time.Second)
	defer cancel()

	addr := e.Request.PathValue("address_hash")
	dist, err := queryTokenDistribution(ctx, p.db, "address_current_token_balances", hexToBytes(addr))
	if err != nil {
		p.app.Logger().Error("token distribution error", slog.String("error", err.Error()))
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to compute distribution"})
	}
	return e.JSON(http.StatusOK, dist)
}

// tokenDistribution serves GET /v1/explorer/tokens/{addr}/distribution (standalone mode).
func (s *StandaloneServer) tokenDistribution(r *http.Request) (any, int) {
	addr := r.PathValue("addr")
	if !isValidHexAddr(addr) {
		return map[string]string{"error": "invalid token address"}, 400
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	dist, err := queryTokenDistribution(ctx, s.db, s.t.balances, addr)
	if err != nil {
		log.Printf("[explorer] distribution error: %v", err)
		return map[string]string{"error": "failed to compute distribution"}, 500
	}
	return dist, 200
}
