// Package p2p implements Perihelion's peer-to-peer layer: block and
// transaction gossip plus initial chain sync over a minimal TCP protocol.
//
// Security posture: peers are untrusted. Every frame is bounded by MaxPayload,
// every block and transaction is fully re-validated by the consensus layer
// before it touches the chain, and a node never executes anything a peer
// sends — messages are data, nothing more.
package p2p

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"perihelion/core"
)

const (
	ProtocolVersion = 1
	MaxPayload      = 4 << 20

	msgHello          = 1
	msgGetBlocks      = 2
	msgBlock          = 3
	msgTx             = 4
	msgGetBlockByHash = 5
	msgGetAddr        = 6
	msgAddr           = 7

	// MaxAddrPerMessage bounds a peer-list message. Address gossip is the
	// classic vector for eclipse attacks — an adversary floods a victim's
	// address book with endpoints it controls until every connection leads
	// back to it — so the amount of address data one peer can inject is
	// capped here and again in the address book.
	MaxAddrPerMessage = 64
	MaxAddrLen        = 64
)

func writeFrame(conn net.Conn, t byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("payload too large")
	}
	hdr := make([]byte, 9)
	binary.BigEndian.PutUint32(hdr[0:4], wireMagic())
	hdr[4] = t
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func readFrame(conn net.Conn) (byte, []byte, error) {
	hdr := make([]byte, 9)
	conn.SetReadDeadline(time.Now().Add(180 * time.Second))
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return 0, nil, err
	}
	if binary.BigEndian.Uint32(hdr[0:4]) != wireMagic() {
		return 0, nil, fmt.Errorf("bad magic — not a perihelion peer")
	}
	n := binary.BigEndian.Uint32(hdr[5:9])
	if n > MaxPayload {
		return 0, nil, fmt.Errorf("oversized payload")
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(conn, p); err != nil {
		return 0, nil, err
	}
	return hdr[4], p, nil
}

// The handshake is APPEND-ONLY. Fields may be added at the end and readers
// must tolerate a payload that ends before them; no existing field may be
// moved, resized or reinterpreted. Peer software is not upgraded in lockstep,
// so a reader that insists on an exact length rejects every newer peer and
// silently partitions the network — which happened once, on 9 August 2026,
// when the listen-address field was introduced. Bump ProtocolVersion instead
// if a genuinely incompatible change is ever required, so that peers refuse
// each other explicitly rather than failing to parse.
//
// encodeHello writes the handshake. Nodes that accept inbound connections
// append the address they listen on so that others can reach them directly;
// nodes that do not listen append nothing and stay unreachable by design.
func encodeHello(ver uint32, height uint64, tip [32]byte, listenAddr string) []byte {
	out := make([]byte, 44, 44+2+len(listenAddr))
	binary.BigEndian.PutUint32(out[0:4], ver)
	binary.BigEndian.PutUint64(out[4:12], height)
	copy(out[12:44], tip[:])
	if len(listenAddr) > MaxAddrLen {
		listenAddr = ""
	}
	out = append(out, byte(len(listenAddr)>>8), byte(len(listenAddr)))
	return append(out, listenAddr...)
}

func decodeHello(p []byte) (ver uint32, height uint64, tip [32]byte, listenAddr string, err error) {
	if len(p) < 44 {
		return 0, 0, tip, "", fmt.Errorf("bad hello")
	}
	copy(tip[:], p[12:44])
	ver = binary.BigEndian.Uint32(p[0:4])
	height = binary.BigEndian.Uint64(p[4:12])
	if len(p) >= 46 {
		n := int(p[44])<<8 | int(p[45])
		if n > MaxAddrLen || 46+n > len(p) {
			return 0, 0, tip, "", fmt.Errorf("bad hello address")
		}
		listenAddr = string(p[46 : 46+n])
	}
	return ver, height, tip, listenAddr, nil
}

func encodeAddrs(addrs []string) []byte {
	if len(addrs) > MaxAddrPerMessage {
		addrs = addrs[:MaxAddrPerMessage]
	}
	out := make([]byte, 0, 2+len(addrs)*24)
	out = append(out, byte(len(addrs)>>8), byte(len(addrs)))
	for _, a := range addrs {
		if len(a) > MaxAddrLen {
			continue
		}
		out = append(out, byte(len(a)))
		out = append(out, a...)
	}
	return out
}

func decodeAddrs(p []byte) ([]string, error) {
	if len(p) < 2 {
		return nil, fmt.Errorf("bad addr message")
	}
	n := int(p[0])<<8 | int(p[1])
	if n > MaxAddrPerMessage {
		return nil, fmt.Errorf("too many addresses")
	}
	out := make([]string, 0, n)
	off := 2
	for i := 0; i < n; i++ {
		if off >= len(p) {
			return nil, fmt.Errorf("truncated addr message")
		}
		l := int(p[off])
		off++
		if l > MaxAddrLen || off+l > len(p) {
			return nil, fmt.Errorf("truncated address")
		}
		out = append(out, string(p[off:off+l]))
		off += l
	}
	return out, nil
}

func encodeGetBlocks(from uint64, count uint32) []byte {
	out := make([]byte, 12)
	binary.BigEndian.PutUint64(out[0:8], from)
	binary.BigEndian.PutUint32(out[8:12], count)
	return out
}

func decodeGetBlocks(p []byte) (uint64, uint32, error) {
	if len(p) != 12 {
		return 0, 0, fmt.Errorf("bad getblocks")
	}
	return binary.BigEndian.Uint64(p[0:8]), binary.BigEndian.Uint32(p[8:12]), nil
}

// wireMagic is the frame prefix for the active network. Nodes on different
// networks (mainnet, testnet, regtest) use different values, so a connection
// between them fails at the very first frame — they cannot accidentally sync
// each other's chains, and a testnet coin can never be mistaken for a mainnet
// one because the two networks are unable to exchange a single message.
func wireMagic() uint32 { return core.Active().WireMagic }

// DefaultPort is the P2P port for the active network.
func DefaultPort() int { return core.Active().DefaultPort }

// DefaultSeeds are the entry points a fresh install dials to find the active
// network. They matter only until a node has peers of its own. Regtest has
// none: it is private by definition.
func DefaultSeeds() []string { return append([]string{}, core.Active().Seeds...) }
