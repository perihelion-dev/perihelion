package p2p_test

import (
	"testing"
	"time"

	"perihelion/p2p"
)

// TestOneHostCannotFillPeerTable reproduces the condition seen on the live
// seed, where one participant held 24 of 32 connection slots and crowded
// everyone else out. Whether such a host is hostile or merely misbehaving, a
// node must not let a single address consume its capacity.
//
// The clients are real nodes completing real handshakes — raw sockets would
// be dropped for not speaking the protocol and would prove nothing.
func TestOneHostCannotFillPeerTable(t *testing.T) {
	srv := p2p.New(newChain(t), nil)
	if err := srv.Start("127.0.0.1:0", nil); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	addr := srv.Addr()

	const clients = 12 // twice the per-host limit
	for i := 0; i < clients; i++ {
		c := p2p.New(newChain(t), nil)
		if err := c.Start("", []string{addr}); err != nil {
			t.Fatal(err)
		}
		defer c.Stop()
	}

	// Watch long enough for every client to have connected and retried.
	deadline := time.Now().Add(20 * time.Second)
	peak := 0
	for time.Now().Before(deadline) {
		if n := srv.PeerCount(); n > peak {
			peak = n
		}
		time.Sleep(200 * time.Millisecond)
	}

	if peak == 0 {
		t.Fatal("no client ever connected — the test proves nothing")
	}
	if peak > p2p.MaxPeersPerHostForTest {
		t.Fatalf("%d clients from one address reached %d slots, above the per-host limit of %d",
			clients, peak, p2p.MaxPeersPerHostForTest)
	}
	t.Logf("%d clients from one address peaked at %d of %d permitted slots",
		clients, peak, p2p.MaxPeersPerHostForTest)
}
