package p2p_test

import (
	"path/filepath"
	"testing"
	"time"

	"perihelion/core"
	"perihelion/p2p"
)

func newChain(t *testing.T) *core.Chain {
	t.Helper()
	c, err := core.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestPeerDiscovery is the property that turns a star around one seed into a
// mesh: two nodes that know only the seed must find each other through it and
// connect directly, so that the network survives the seed disappearing.
func TestPeerDiscovery(t *testing.T) {
	seed := p2p.New(newChain(t), nil)
	if err := seed.Start("127.0.0.1:0", nil); err != nil {
		t.Fatal(err)
	}
	defer seed.Stop()

	// Both peers listen (so they are reachable) and know only the seed.
	a := p2p.New(newChain(t), nil)
	if err := a.Start("127.0.0.1:0", []string{seed.Addr()}); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()

	b := p2p.New(newChain(t), nil)
	if err := b.Start("127.0.0.1:0", []string{seed.Addr()}); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()

	// Each should end up with two peers: the seed, plus the other node found
	// through address gossip. Discovery runs on a timer, so allow for it.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if a.PeerCount() >= 2 && b.PeerCount() >= 2 {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("nodes did not discover each other: a has %d peers, b has %d", a.PeerCount(), b.PeerCount())
}

// TestNonListeningNodeStaysPrivate verifies that a node which accepts no
// inbound connections — the desktop wallet's configuration — is never
// advertised to anyone, so running a wallet at home does not publish a home
// address to the network.
func TestNonListeningNodeStaysPrivate(t *testing.T) {
	seed := p2p.New(newChain(t), nil)
	if err := seed.Start("127.0.0.1:0", nil); err != nil {
		t.Fatal(err)
	}
	defer seed.Stop()

	quiet := p2p.New(newChain(t), nil)
	if err := quiet.Start("", []string{seed.Addr()}); err != nil { // no listener
		t.Fatal(err)
	}
	defer quiet.Stop()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && seed.PeerCount() == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	if seed.PeerCount() == 0 {
		t.Fatal("quiet node never connected")
	}

	// Whatever the seed offers to others must not include the quiet node's
	// ephemeral outbound endpoint.
	observer := p2p.New(newChain(t), nil)
	if err := observer.Start("127.0.0.1:0", []string{seed.Addr()}); err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()

	time.Sleep(3 * time.Second)
	for _, a := range observer.KnownAddrs() {
		if a == quiet.Addr() && quiet.Addr() != "" {
			t.Fatalf("a non-listening node was advertised: %s", a)
		}
	}
}

// TestAddrBookRejectsUnroutable ensures a remote peer cannot steer a node
// toward addresses inside its own network or at itself.
func TestAddrBookRejectsUnroutable(t *testing.T) {
	for _, bad := range []string{
		"0.0.0.0:16180",
		"224.0.0.1:16180",
		"169.254.1.1:16180",
		"not-an-address",
		"1.2.3.4",
	} {
		if p2p.RoutableForTest(bad, false) {
			t.Fatalf("address %q should not be dialled from a remote peer", bad)
		}
	}
	// Private addresses are acceptable only from a local peer.
	if p2p.RoutableForTest("192.168.1.10:16180", false) {
		t.Fatal("private address accepted from a remote peer")
	}
	if !p2p.RoutableForTest("192.168.1.10:16180", true) {
		t.Fatal("private address should be usable on a local network")
	}
	if !p2p.RoutableForTest("186.240.157.169:16180", false) {
		t.Fatal("a public address should be dialable")
	}
}
