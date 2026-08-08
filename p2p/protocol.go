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
)

const (
	// Magic prefixes every frame; connections speaking anything else are dropped.
	Magic           = 0x50455231 // "PER1"
	ProtocolVersion = 1
	MaxPayload      = 4 << 20
	DefaultPort     = 16180

	msgHello          = 1
	msgGetBlocks      = 2
	msgBlock          = 3
	msgTx             = 4
	msgGetBlockByHash = 5
)

func writeFrame(conn net.Conn, t byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("payload too large")
	}
	hdr := make([]byte, 9)
	binary.BigEndian.PutUint32(hdr[0:4], Magic)
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
	if binary.BigEndian.Uint32(hdr[0:4]) != Magic {
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

func encodeHello(ver uint32, height uint64, tip [32]byte) []byte {
	out := make([]byte, 44)
	binary.BigEndian.PutUint32(out[0:4], ver)
	binary.BigEndian.PutUint64(out[4:12], height)
	copy(out[12:44], tip[:])
	return out
}

func decodeHello(p []byte) (uint32, uint64, [32]byte, error) {
	var tip [32]byte
	if len(p) != 44 {
		return 0, 0, tip, fmt.Errorf("bad hello")
	}
	copy(tip[:], p[12:44])
	return binary.BigEndian.Uint32(p[0:4]), binary.BigEndian.Uint64(p[4:12]), tip, nil
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
