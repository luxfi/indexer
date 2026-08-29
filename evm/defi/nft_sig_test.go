package defi

import (
	"strings"
	"testing"

	"golang.org/x/crypto/sha3"
)

// An event topic is keccak256 of the event's canonical signature. That makes
// every constant in nft.go derivable from the text already written beside it,
// so a test can check the two agree rather than a reader taking the hex on
// faith.
//
// It is here because they did not agree. Seventeen of twenty-three were wrong,
// and the wrong ones were not typos: ZoraAskCreated and ZoraAskCancelled were
// the same sixty-three characters with a different first byte, Foundation's
// settled topic was "e6" written thirty-two times, and Blur's cancel matched
// the real hash for eighteen characters before diverging — a remembered prefix
// with the rest filled in. The six that were right were Seaport's and
// LooksRare v1's, which is to say the ones anybody could have copied.
//
// A wrong topic does not fail. It matches no log, so an indexer using it
// reports that the marketplace has never traded, and the difference between
// that and a broken constant is invisible from outside. Hence a test.

func topic(sig string) string {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	sum := h.Sum(nil)
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 66)
	out = append(out, '0', 'x')
	for _, b := range sum {
		out = append(out, hex[b>>4], hex[b&0x0f])
	}
	return string(out)
}

// Each case is the constant and the signature its own comment names. Adding a
// marketplace means adding a line here; leaving one out is the only way to get
// an unchecked constant back, and that is a visible omission rather than a
// silent one.
var derived = []struct {
	name string
	got  string
	sig  string
}{
	{"SeaportOrderFulfilled", SeaportOrderFulfilledSig, "OrderFulfilled(bytes32,address,address,address,(uint8,address,uint256,uint256)[],(uint8,address,uint256,uint256,address)[])"},
	{"SeaportOrderCancelled", SeaportOrderCancelledSig, "OrderCancelled(bytes32,address,address)"},
	{"SeaportCounterIncrement", SeaportCounterIncrementSig, "CounterIncremented(uint256,address)"},

	{"LooksRareTakerBid", LooksRareTakerBidSig, "TakerBid(bytes32,uint256,address,address,address,address,address,uint256,uint256,uint256)"},
	{"LooksRareTakerAsk", LooksRareTakerAskSig, "TakerAsk(bytes32,uint256,address,address,address,address,address,uint256,uint256,uint256)"},
	{"LooksRareCancelAll", LooksRareCancelAllSig, "CancelAllOrders(address,uint256)"},

	{"LooksRareV2TakerBid", LooksRareV2TakerBidSig, "TakerBid((bytes32,uint256,address,address,bool,address,address,uint256,uint256,uint256,uint256[],(bytes32,bytes)[]))"},
	{"LooksRareV2TakerAsk", LooksRareV2TakerAskSig, "TakerAsk((bytes32,uint256,address,address,bool,address,address,uint256,uint256,uint256,uint256[],(bytes32,bytes)[]))"},

	{"BlurOrderCancelled", BlurOrderCancelledSig, "OrderCancelled(bytes32)"},
	{"BlurNonceIncremented", BlurNonceIncrementedSig, "NonceIncremented(address,uint256)"},

	{"RaribleCancel", RaribleCancelSig, "Cancel(bytes32)"},

	{"X2Y2Cancel", X2Y2CancelSig, "EvCancel(bytes32)"},

	{"FoundationBuy", FoundationBuySig, "ReserveAuctionBidPlaced(uint256,address,uint256,uint256)"},
	{"FoundationSettled", FoundationSettledSig, "ReserveAuctionFinalized(uint256,address,address,uint256,uint256,uint256)"},

	{"SuperRareSold", SuperRareSoldSig, "Sold(address,address,uint256,uint256)"},
	{"SuperRareOffer", SuperRareOfferSig, "OfferAccepted(address,address,uint256,uint256)"},

	{"ZoraAskFilled", ZoraAskFilledSig, "AskFilled(address,address,address,uint256,uint256,address,address,uint256)"},
	{"ZoraAskCancelled", ZoraAskCancelledSig, "AskCancelled(address,uint256)"},
}

func TestTopicsAreTheHashOfTheirSignature(t *testing.T) {
	for _, c := range derived {
		if want := topic(c.sig); c.got != want {
			t.Errorf("%s\n  is   %s\n  want %s\n  from %s", c.name, c.got, want, c.sig)
		}
	}
}

// The shapes the wrong ones had. A hash is not compressible, so a topic built
// from a short repeating unit was written by hand, and one that shares a long
// head with a sibling was written by copying it. Neither can survive here.
func TestNoTopicWasWrittenByHand(t *testing.T) {
	for _, c := range derived {
		h := strings.TrimPrefix(c.got, "0x")
		for _, unit := range []int{2, 4, 8} {
			seen := map[string]bool{}
			for i := 0; i+unit <= len(h); i += unit {
				seen[h[i:i+unit]] = true
			}
			if len(seen) <= 3 {
				t.Errorf("%s repeats a %d-character unit: %s", c.name, unit, c.got)
			}
		}
	}
	for i, a := range derived {
		for _, b := range derived[i+1:] {
			n := 0
			for n < len(a.got) && n < len(b.got) && a.got[n] == b.got[n] {
				n++
			}
			if n > 10 {
				t.Errorf("%s and %s share their first %d characters", a.name, b.name, n)
			}
		}
	}
}
