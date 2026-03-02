package defi

import (
	"math/big"
	"testing"
)

func TestDAOTopicsLength(t *testing.T) {
	topics := DAOTopics()
	if len(topics) != 18 {
		t.Fatalf("expected 18 DAO topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseGaugeAdded(t *testing.T) {
	gaugeID := "0x0000000000000000000000000000000000000000000000000000000000000001"
	data := "0x" +
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // recipient
		"0000000000000000000000000000000000000000000000000000000000000040" + // string offset
		"0000000000000000000000000000000000000000000000000000000000000004" + // string length
		"4275726e00000000000000000000000000000000000000000000000000000000" // "Burn"

	logs := []Log{{
		Address:     "0x5555555555555555555555555555555555555555",
		Topics:      []string{TopicGaugeAdded, gaugeID},
		Data:        data,
		BlockNumber: 100,
		TxHash:      "0xaaa",
	}}

	gauges, _, _, _ := ParseDAOEvents(logs)
	if len(gauges) != 1 {
		t.Fatalf("expected 1 gauge, got %d", len(gauges))
	}
	g := gauges[0]
	if g.GaugeID.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("gaugeId = %s, want 1", g.GaugeID)
	}
	if g.Event != "added" {
		t.Errorf("event = %s, want added", g.Event)
	}
	if g.Recipient != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("recipient = %s", g.Recipient)
	}
}

func TestParseGaugeVote(t *testing.T) {
	user := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	gaugeID := "0x0000000000000000000000000000000000000000000000000000000000000002"
	data := "0x00000000000000000000000000000000000000000000000000000000000007d0" // weight = 2000

	logs := []Log{{
		Topics: []string{TopicGaugeVoteCast, user, gaugeID},
		Data:   data,
	}}

	gauges, _, _, _ := ParseDAOEvents(logs)
	if len(gauges) != 1 {
		t.Fatalf("expected 1 gauge vote, got %d", len(gauges))
	}
	g := gauges[0]
	if g.User != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("user = %s", g.User)
	}
	if g.GaugeID.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("gaugeId = %s, want 2", g.GaugeID)
	}
	if g.Weight.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("weight = %s, want 2000", g.Weight)
	}
	if g.Event != "vote" {
		t.Errorf("event = %s, want vote", g.Event)
	}
}

func TestParseVLUXDeposit(t *testing.T) {
	user := "0x000000000000000000000000cccccccccccccccccccccccccccccccccccccccc"
	lockEnd := "0x0000000000000000000000000000000000000000000000000000000065f5e100"
	data := "0x" +
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amount = 1000e18
		"0000000000000000000000000000000000000000000000000000000000278d00" + // lockTime
		"0000000000000000000000000000000000000000000000000000000065f5e100" // ts

	logs := []Log{{
		Topics: []string{TopicVLUXDeposit, user, lockEnd},
		Data:   data,
	}}

	_, vlux, _, _ := ParseDAOEvents(logs)
	if len(vlux) != 1 {
		t.Fatalf("expected 1 vLUX deposit, got %d", len(vlux))
	}
	v := vlux[0]
	if v.User != "0xcccccccccccccccccccccccccccccccccccccccc" {
		t.Errorf("user = %s", v.User)
	}
	if v.Event != "deposit" {
		t.Errorf("event = %s, want deposit", v.Event)
	}
	expected := new(big.Int)
	expected.SetString("3635c9adc5dea00000", 16)
	if v.Amount.Cmp(expected) != 0 {
		t.Errorf("amount = %s, want %s", v.Amount, expected)
	}
}

func TestParseVLUXWithdraw(t *testing.T) {
	user := "0x000000000000000000000000dddddddddddddddddddddddddddddddddddddddd"
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amount = 100e18
		"0000000000000000000000000000000000000000000000000000000065f5e100" // ts

	logs := []Log{{
		Topics: []string{TopicVLUXWithdraw, user},
		Data:   data,
	}}

	_, vlux, _, _ := ParseDAOEvents(logs)
	if len(vlux) != 1 || vlux[0].Event != "withdraw" {
		t.Fatalf("expected 1 vLUX withdraw")
	}
}

func TestParseKarmaMinted(t *testing.T) {
	to := "0x000000000000000000000000eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	data := "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amount = 100e18
		"4449445f564552494649434154494f4e000000000000000000000000000000000" // reason (truncated hash)

	// Fix data to be word-aligned
	data = "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" + // amount
		"4449445f564552494649434154494f4e0000000000000000000000000000000000" // reason bytes32

	// Actually: just use clean 2-word data
	data = "0x" +
		"0000000000000000000000000000000000000000000000056bc75e2d63100000" +
		"4449445f564552494649434154494f4e000000000000000000000000000000ff"

	logs := []Log{{
		Topics: []string{TopicKarmaMinted, to},
		Data:   data,
	}}

	_, _, karma, _ := ParseDAOEvents(logs)
	if len(karma) != 1 {
		t.Fatalf("expected 1 karma event, got %d", len(karma))
	}
	k := karma[0]
	if k.Event != "minted" {
		t.Errorf("event = %s, want minted", k.Event)
	}
	if k.Account != "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" {
		t.Errorf("account = %s", k.Account)
	}
}

func TestParseKarmaDecayed(t *testing.T) {
	account := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := "0x" +
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" + // amount = 1e18
		"0000000000000000000000000000000000000000000000000000000000000001" // wasActive = true

	logs := []Log{{
		Topics: []string{TopicKarmaDecayed, account},
		Data:   data,
	}}

	_, _, karma, _ := ParseDAOEvents(logs)
	if len(karma) != 1 || karma[0].Event != "decayed" {
		t.Fatal("expected decayed event")
	}
	if !karma[0].WasActive {
		t.Error("expected wasActive = true")
	}
}

func TestParseDIDLinked(t *testing.T) {
	account := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	did := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	logs := []Log{{
		Topics: []string{TopicDIDLinked, account, did},
		Data:   "0x",
	}}

	_, _, karma, _ := ParseDAOEvents(logs)
	if len(karma) != 1 || karma[0].Event != "did_linked" {
		t.Fatal("expected did_linked event")
	}
	if karma[0].DID != did {
		t.Errorf("did = %s, want %s", karma[0].DID, did)
	}
}

func TestParseDLUXStaked(t *testing.T) {
	user := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := "0x" +
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // amount = 1000e18
		"0000000000000000000000000000000000000000000000000000000000000002" + // tier = 2 (Gold)
		"0000000000000000000000000000000000000000000000000000000065f5e100" // lockEnd

	logs := []Log{{
		Topics: []string{TopicDLUXStaked, user},
		Data:   data,
	}}

	_, _, _, dlux := ParseDAOEvents(logs)
	if len(dlux) != 1 || dlux[0].Event != "staked" {
		t.Fatal("expected staked event")
	}
	if dlux[0].Tier != 2 {
		t.Errorf("tier = %d, want 2", dlux[0].Tier)
	}
}

func TestParseRebased(t *testing.T) {
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000001" + // epoch = 1
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" + // totalRebased
		"0000000000000000000000000000000000000000000000000000000000000028" // rate = 40 bps

	logs := []Log{{
		Topics: []string{TopicRebased},
		Data:   data,
	}}

	_, _, _, dlux := ParseDAOEvents(logs)
	if len(dlux) != 1 || dlux[0].Event != "rebased" {
		t.Fatal("expected rebased event")
	}
	if dlux[0].Epoch.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("epoch = %s, want 1", dlux[0].Epoch)
	}
	if dlux[0].Rate.Cmp(big.NewInt(40)) != 0 {
		t.Errorf("rate = %s, want 40", dlux[0].Rate)
	}
}

func TestParseDLUXMinted(t *testing.T) {
	user := "0x000000000000000000000000cccccccccccccccccccccccccccccccccccccccc"
	reason := "0xabcdef0000000000000000000000000000000000000000000000000000000000"
	data := "0x0000000000000000000000000000000000000000000000056bc75e2d63100000" // amount

	logs := []Log{{
		Topics: []string{TopicDLUXMinted, user, reason},
		Data:   data,
	}}

	_, _, _, dlux := ParseDAOEvents(logs)
	if len(dlux) != 1 || dlux[0].Event != "minted" {
		t.Fatal("expected minted event")
	}
	if dlux[0].Reason != reason {
		t.Errorf("reason = %s", dlux[0].Reason)
	}
}

func TestWeightsUpdated(t *testing.T) {
	data := "0x0000000000000000000000000000000000000000000000000000000065f5e100"
	logs := []Log{{
		Topics: []string{TopicWeightsUpdated},
		Data:   data,
	}}

	gauges, _, _, _ := ParseDAOEvents(logs)
	if len(gauges) != 1 || gauges[0].Event != "weights_updated" {
		t.Fatal("expected weights_updated event")
	}
}

func TestEmptyDAOLogs(t *testing.T) {
	gauges, vlux, karma, dlux := ParseDAOEvents(nil)
	if gauges != nil || vlux != nil || karma != nil || dlux != nil {
		t.Error("expected nil results for nil input")
	}
}
