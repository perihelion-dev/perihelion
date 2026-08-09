package p2p

import (
	"encoding/binary"
	"testing"
)

// TestHelloIsAppendOnly guards the wire-compatibility rule. A handshake from
// an older node — which ends after the fixed 44-byte prefix — must still parse,
// and so must one carrying fields this version does not know about. Breaking
// this partitions the network along software versions.
func TestHelloIsAppendOnly(t *testing.T) {
	var tip [32]byte
	for i := range tip {
		tip[i] = byte(i)
	}

	// A legacy 44-byte handshake, exactly as older nodes emit it.
	legacy := make([]byte, 44)
	binary.BigEndian.PutUint32(legacy[0:4], ProtocolVersion)
	binary.BigEndian.PutUint64(legacy[4:12], 4242)
	copy(legacy[12:44], tip[:])

	ver, height, gotTip, addr, err := decodeHello(legacy)
	if err != nil {
		t.Fatalf("legacy handshake rejected: %v", err)
	}
	if ver != ProtocolVersion || height != 4242 || gotTip != tip || addr != "" {
		t.Fatalf("legacy handshake misparsed: ver=%d height=%d addr=%q", ver, height, addr)
	}

	// A handshake from a future version carrying an extra trailing field must
	// still yield the fields this version understands.
	future := append(encodeHello(ProtocolVersion, 7, tip, "1.2.3.4:16180"), 0xff, 0xff, 0xff)
	ver, height, gotTip, addr, err = decodeHello(future)
	if err != nil {
		t.Fatalf("handshake with unknown trailing data rejected: %v", err)
	}
	if height != 7 || gotTip != tip || addr != "1.2.3.4:16180" {
		t.Fatalf("future handshake misparsed: height=%d addr=%q", height, addr)
	}

	// Truncated payloads must still be refused.
	if _, _, _, _, err := decodeHello(legacy[:43]); err == nil {
		t.Fatal("truncated handshake accepted")
	}
	// A length prefix that overruns the payload must be refused.
	bad := append(append([]byte{}, legacy...), 0x00, 0x20)
	if _, _, _, _, err := decodeHello(bad); err == nil {
		t.Fatal("handshake with overrunning address length accepted")
	}
}

func TestAddrEncodingRoundtrip(t *testing.T) {
	in := []string{"1.2.3.4:16180", "example.org:16180", "[2001:db8::1]:16180"}
	out, err := decodeAddrs(encodeAddrs(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d addresses, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("address %d = %q, want %q", i, out[i], in[i])
		}
	}
	if _, err := decodeAddrs([]byte{0x00}); err == nil {
		t.Fatal("truncated addr message accepted")
	}
	if _, err := decodeAddrs([]byte{0xff, 0xff}); err == nil {
		t.Fatal("oversized address count accepted")
	}
}
