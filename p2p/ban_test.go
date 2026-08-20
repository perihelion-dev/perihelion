package p2p

import (
	"net"
	"testing"
)

// TestPenalizeBansWithoutPanic reproduces the crash of 2026-08-18: the banned
// map was never initialised in New, so the first peer ever to cross the ban
// threshold — which first happened during the chain-ID fork, when peers
// streamed dozens of rejected blocks — panicked the node with "assignment to
// entry in nil map" instead of being banned. The network kept the node alive
// only because systemd restarted it after every crash.
func TestPenalizeBansWithoutPanic(t *testing.T) {
	n := New(nil, nil)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	p := &peer{name: "203.0.113.7:1234", conn: c1}

	// Cross the threshold the way the fork did: repeated invalid blocks. One
	// more than the exact quotient, because the score decays between calls
	// and an exact sum lands epsilon below the threshold.
	for i := 0; i <= banThreshold/penaltyInvalidBlock; i++ {
		n.penalize(p, penaltyInvalidBlock, "invalid block")
	}
	if !n.isBanned("203.0.113.7") {
		t.Fatal("peer crossed the ban threshold but is not banned")
	}
	// A configured seed must never be banned, however badly it scores.
	n.seedAddrs = []string{"198.51.100.9:16180"}
	c3, c4 := net.Pipe()
	defer c3.Close()
	defer c4.Close()
	sp := &peer{name: "198.51.100.9:16180", conn: c3}
	for i := 0; i < 2*banThreshold/penaltyInvalidBlock; i++ {
		n.penalize(sp, penaltyInvalidBlock, "invalid block")
	}
	if n.isBanned("198.51.100.9") {
		t.Fatal("a configured seed was banned")
	}
}
