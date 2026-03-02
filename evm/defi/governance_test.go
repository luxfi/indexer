package defi

import (
	"math/big"
	"testing"
)

func TestGovernanceTopicsLength(t *testing.T) {
	topics := GovernanceTopics()
	if len(topics) != 8 {
		t.Fatalf("expected 8 governance topics, got %d", len(topics))
	}
	for _, topic := range topics {
		if len(topic) != 66 || topic[:2] != "0x" {
			t.Errorf("invalid topic format: %s", topic)
		}
	}
}

func TestParseProposalCreated(t *testing.T) {
	// ProposalCreated: data has proposalId at word 0, proposer at word 1, startBlock at word 5, endBlock at word 6
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000007" + // word 0: proposalId = 7
		"000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + // word 1: proposer
		"0000000000000000000000000000000000000000000000000000000000000000" + // word 2: placeholder
		"0000000000000000000000000000000000000000000000000000000000000000" + // word 3: placeholder
		"0000000000000000000000000000000000000000000000000000000000000000" + // word 4: placeholder
		"0000000000000000000000000000000000000000000000000000000000000064" + // word 5: startBlock = 100
		"00000000000000000000000000000000000000000000000000000000000000c8" // word 6: endBlock = 200

	logs := []Log{{
		Address:     "0x1111111111111111111111111111111111111111",
		Topics:      []string{TopicProposalCreated},
		Data:        data,
		BlockNumber: 50,
		TxHash:      "0xabc",
	}}

	proposals, votes, delegates, dvotes := ParseGovernanceEvents(logs)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	if len(votes) != 0 || len(delegates) != 0 || len(dvotes) != 0 {
		t.Fatal("expected no votes/delegates")
	}

	p := proposals[0]
	if p.ProposalID.Cmp(big.NewInt(7)) != 0 {
		t.Errorf("proposalId = %s, want 7", p.ProposalID)
	}
	if p.Proposer != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("proposer = %s", p.Proposer)
	}
	if p.StartBlock.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("startBlock = %s, want 100", p.StartBlock)
	}
	if p.EndBlock.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("endBlock = %s, want 200", p.EndBlock)
	}
	if p.Event != "created" {
		t.Errorf("event = %s, want created", p.Event)
	}
	if p.Block != 50 {
		t.Errorf("block = %d, want 50", p.Block)
	}
}

func TestParseVoteCast(t *testing.T) {
	// VoteCast(address indexed voter, uint256 proposalId, uint8 support, uint256 votes, string reason)
	voter := "0x000000000000000000000000bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000003" + // proposalId = 3
		"0000000000000000000000000000000000000000000000000000000000000001" + // support = 1 (For)
		"00000000000000000000000000000000000000000000003635c9adc5dea00000" // votes = 1000e18

	logs := []Log{{
		Address:     "0x2222222222222222222222222222222222222222",
		Topics:      []string{TopicVoteCast, voter},
		Data:        data,
		BlockNumber: 60,
		TxHash:      "0xdef",
	}}

	_, votes, _, _ := ParseGovernanceEvents(logs)
	if len(votes) != 1 {
		t.Fatalf("expected 1 vote, got %d", len(votes))
	}

	v := votes[0]
	if v.Voter != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("voter = %s", v.Voter)
	}
	if v.ProposalID.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("proposalId = %s, want 3", v.ProposalID)
	}
	if v.Support != 1 {
		t.Errorf("support = %d, want 1", v.Support)
	}
	expected := new(big.Int)
	expected.SetString("3635c9adc5dea00000", 16)
	if v.Weight.Cmp(expected) != 0 {
		t.Errorf("weight = %s, want %s", v.Weight, expected)
	}
}

func TestParseProposalExecuted(t *testing.T) {
	data := "0x0000000000000000000000000000000000000000000000000000000000000005"
	logs := []Log{{
		Topics: []string{TopicProposalExecuted},
		Data:   data,
	}}

	proposals, _, _, _ := ParseGovernanceEvents(logs)
	if len(proposals) != 1 || proposals[0].Event != "executed" {
		t.Fatal("expected executed proposal")
	}
	if proposals[0].ProposalID.Cmp(big.NewInt(5)) != 0 {
		t.Errorf("proposalId = %s, want 5", proposals[0].ProposalID)
	}
}

func TestParseProposalQueued(t *testing.T) {
	data := "0x" +
		"000000000000000000000000000000000000000000000000000000000000000a" + // proposalId = 10
		"0000000000000000000000000000000000000000000000000000000065b8d800" // eta

	logs := []Log{{
		Topics: []string{TopicProposalQueued},
		Data:   data,
	}}

	proposals, _, _, _ := ParseGovernanceEvents(logs)
	if len(proposals) != 1 || proposals[0].Event != "queued" {
		t.Fatal("expected queued proposal")
	}
	if proposals[0].ProposalID.Cmp(big.NewInt(10)) != 0 {
		t.Errorf("proposalId = %s, want 10", proposals[0].ProposalID)
	}
	if proposals[0].ETA == nil || proposals[0].ETA.Sign() == 0 {
		t.Error("expected non-zero ETA")
	}
}

func TestParseDelegateChanged(t *testing.T) {
	logs := []Log{{
		Topics: []string{
			TopicDelegateChanged,
			"0x0000000000000000000000001111111111111111111111111111111111111111", // delegator
			"0x0000000000000000000000002222222222222222222222222222222222222222", // from
			"0x0000000000000000000000003333333333333333333333333333333333333333", // to
		},
		Data: "0x",
	}}

	_, _, delegates, _ := ParseGovernanceEvents(logs)
	if len(delegates) != 1 {
		t.Fatalf("expected 1 delegate change, got %d", len(delegates))
	}
	d := delegates[0]
	if d.Delegator != "0x1111111111111111111111111111111111111111" {
		t.Errorf("delegator = %s", d.Delegator)
	}
	if d.FromDelegate != "0x2222222222222222222222222222222222222222" {
		t.Errorf("from = %s", d.FromDelegate)
	}
	if d.ToDelegate != "0x3333333333333333333333333333333333333333" {
		t.Errorf("to = %s", d.ToDelegate)
	}
}

func TestParseDelegateVotesChanged(t *testing.T) {
	data := "0x" +
		"00000000000000000000000000000000000000000000000000000000000003e8" + // prev = 1000
		"00000000000000000000000000000000000000000000000000000000000007d0" // new = 2000

	logs := []Log{{
		Topics: []string{
			TopicDelegateVotesChanged,
			"0x0000000000000000000000004444444444444444444444444444444444444444",
		},
		Data: data,
	}}

	_, _, _, dvotes := ParseGovernanceEvents(logs)
	if len(dvotes) != 1 {
		t.Fatalf("expected 1 delegate votes change, got %d", len(dvotes))
	}
	d := dvotes[0]
	if d.PreviousBalance.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("prev = %s, want 1000", d.PreviousBalance)
	}
	if d.NewBalance.Cmp(big.NewInt(2000)) != 0 {
		t.Errorf("new = %s, want 2000", d.NewBalance)
	}
}

func TestDecodeWord(t *testing.T) {
	data := "0x0000000000000000000000000000000000000000000000000000000000000042"
	w := decodeWord(data, 0)
	if w.Cmp(big.NewInt(0x42)) != 0 {
		t.Errorf("got %s, want 66", w)
	}

	// Short data
	short := decodeWord("0x00", 0)
	if short.Sign() != 0 {
		t.Errorf("short data: got %s, want 0", short)
	}
}

func TestDecodeAddress(t *testing.T) {
	data := "0x000000000000000000000000abcdef0123456789abcdef0123456789abcdef01"
	addr := decodeAddress(data, 0)
	if addr != "0xabcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("got %s", addr)
	}
}

func TestTopicAddress(t *testing.T) {
	topic := "0x000000000000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addr := topicAddress(topic)
	if addr != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("got %s", addr)
	}
}

func TestEmptyLogs(t *testing.T) {
	proposals, votes, delegates, dvotes := ParseGovernanceEvents(nil)
	if proposals != nil || votes != nil || delegates != nil || dvotes != nil {
		t.Error("expected nil results for nil input")
	}

	proposals, votes, delegates, dvotes = ParseGovernanceEvents([]Log{{Topics: []string{}}})
	if proposals != nil || votes != nil || delegates != nil || dvotes != nil {
		t.Error("expected nil results for empty topics")
	}
}
