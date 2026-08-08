package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"perihelion/core"
)

const (
	maxPeers    = 32
	maxOrphans  = 128
	maxSeenTx   = 16384
	syncBatch   = 200
	reqThrottle = 2 * time.Second
)

// Node gossips blocks and transactions with configured peers and keeps the
// local chain in sync. It only ever connects to addresses the operator
// explicitly configured — there is no peer discovery, no NAT punching, no
// hidden endpoints.
type Node struct {
	chain *core.Chain
	logf  func(string, ...any)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	peers   map[string]*peer
	orphans map[[32]byte][]byte     // block hash -> raw block
	waiting map[[32]byte][][32]byte // parent hash -> orphans waiting for it
	seenTx  map[[32]byte]struct{}

	lnMu     sync.Mutex
	listener net.Listener
}

type peer struct {
	name string
	conn net.Conn

	wmu sync.Mutex // serializes frame writes

	mu      sync.Mutex
	height  uint64
	lastReq time.Time
}

func (p *peer) send(t byte, payload []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	return writeFrame(p.conn, t, payload)
}

func New(chain *core.Chain, logf func(string, ...any)) *Node {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Node{
		chain:   chain,
		logf:    logf,
		ctx:     ctx,
		cancel:  cancel,
		peers:   map[string]*peer{},
		orphans: map[[32]byte][]byte{},
		waiting: map[[32]byte][][32]byte{},
		seenTx:  map[[32]byte]struct{}{},
	}
}

// Start begins listening (if listen != "") and dials the configured peers,
// reconnecting with backoff for as long as the node runs.
func (n *Node) Start(listen string, connect []string) error {
	if listen != "" {
		ln, err := net.Listen("tcp", listen)
		if err != nil {
			return err
		}
		n.lnMu.Lock()
		n.listener = ln
		n.lnMu.Unlock()
		n.logf("p2p: listening on %s", ln.Addr())
		n.wg.Add(1)
		go n.acceptLoop(ln)
	}
	for _, addr := range connect {
		addr := addr
		n.wg.Add(1)
		go n.dialLoop(addr)
	}
	n.wg.Add(1)
	go n.keepaliveLoop()
	return nil
}

// Addr returns the actual listen address (useful with ":0" in tests).
func (n *Node) Addr() string {
	n.lnMu.Lock()
	defer n.lnMu.Unlock()
	if n.listener == nil {
		return ""
	}
	return n.listener.Addr().String()
}

func (n *Node) Stop() {
	n.cancel()
	n.lnMu.Lock()
	if n.listener != nil {
		n.listener.Close()
	}
	n.lnMu.Unlock()
	n.mu.Lock()
	for _, p := range n.peers {
		p.conn.Close()
	}
	n.mu.Unlock()
	n.wg.Wait()
}

func (n *Node) PeerCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.peers)
}

func (n *Node) acceptLoop(ln net.Listener) {
	defer n.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		n.mu.Lock()
		full := len(n.peers) >= maxPeers
		n.mu.Unlock()
		if full {
			conn.Close()
			continue
		}
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			n.handleConn(conn)
		}()
	}
}

func (n *Node) dialLoop(addr string) {
	defer n.wg.Done()
	for n.ctx.Err() == nil {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err == nil {
			n.handleConn(conn)
		} else {
			n.logf("p2p: cannot reach %s (%v), retrying", addr, err)
		}
		select {
		case <-n.ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// keepaliveLoop refreshes peer height knowledge and re-gossips pending
// transactions so nothing starves if a frame was missed.
func (n *Node) keepaliveLoop() {
	defer n.wg.Done()
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			n.broadcast(msgHello, n.helloPayload(), nil)
			for _, raw := range n.chain.MempoolRaw() {
				n.broadcast(msgTx, raw, nil)
			}
		}
	}
}

func (n *Node) helloPayload() []byte {
	height, tip, err := n.chain.TipInfo()
	if err != nil {
		return encodeHello(ProtocolVersion, 0, [32]byte{})
	}
	return encodeHello(ProtocolVersion, height, tip)
}

// broadcast sends a frame to every connected peer except skip.
func (n *Node) broadcast(t byte, payload []byte, skip *peer) {
	n.mu.Lock()
	ps := make([]*peer, 0, len(n.peers))
	for _, p := range n.peers {
		if p != skip {
			ps = append(ps, p)
		}
	}
	n.mu.Unlock()
	for _, p := range ps {
		if err := p.send(t, payload); err != nil {
			p.conn.Close()
		}
	}
}

func (n *Node) handleConn(conn net.Conn) {
	defer conn.Close()
	p := &peer{name: conn.RemoteAddr().String(), conn: conn}
	if err := p.send(msgHello, n.helloPayload()); err != nil {
		return
	}
	t, payload, err := readFrame(conn)
	if err != nil || t != msgHello {
		return
	}
	ver, height, _, err := decodeHello(payload)
	if err != nil || ver != ProtocolVersion {
		return
	}
	p.mu.Lock()
	p.height = height
	p.mu.Unlock()

	n.mu.Lock()
	if _, dup := n.peers[p.name]; dup || len(n.peers) >= maxPeers {
		n.mu.Unlock()
		return
	}
	n.peers[p.name] = p
	n.mu.Unlock()
	n.logf("p2p: connected to %s (height %d)", p.name, height)
	defer func() {
		n.mu.Lock()
		delete(n.peers, p.name)
		n.mu.Unlock()
		n.logf("p2p: disconnected from %s", p.name)
	}()

	n.maybeSync(p)
	for {
		t, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		if err := n.dispatch(p, t, payload); err != nil {
			return
		}
	}
}

// maybeSync requests a batch of blocks when the peer claims a longer chain
// (throttled so a stream of arriving blocks does not cause request storms).
func (n *Node) maybeSync(p *peer) {
	ourHeight, _, err := n.chain.TipInfo()
	if err != nil {
		return
	}
	p.mu.Lock()
	should := p.height > ourHeight && time.Since(p.lastReq) >= reqThrottle
	if should {
		p.lastReq = time.Now()
	}
	p.mu.Unlock()
	if should {
		p.send(msgGetBlocks, encodeGetBlocks(ourHeight+1, syncBatch))
	}
}

func (n *Node) dispatch(p *peer, t byte, payload []byte) error {
	switch t {
	case msgHello:
		_, height, _, err := decodeHello(payload)
		if err != nil {
			return err
		}
		p.mu.Lock()
		p.height = height
		p.mu.Unlock()
		n.maybeSync(p)
	case msgGetBlocks:
		from, count, err := decodeGetBlocks(payload)
		if err != nil {
			return err
		}
		raws, err := n.chain.RawBlocksFrom(from, int(count))
		if err != nil {
			return nil
		}
		for _, raw := range raws {
			if err := p.send(msgBlock, raw); err != nil {
				return err
			}
		}
	case msgBlock:
		n.handleBlock(p, payload)
	case msgTx:
		n.handleTx(p, payload)
	case msgGetBlockByHash:
		if len(payload) != 32 {
			return fmt.Errorf("bad block request")
		}
		var h [32]byte
		copy(h[:], payload)
		if raw, ok := n.chain.RawBlockByHash(h); ok {
			return p.send(msgBlock, raw)
		}
	default:
		// unknown message type: ignored for forward compatibility
	}
	return nil
}

func (n *Node) handleBlock(p *peer, raw []byte) {
	b, err := core.DeserializeBlock(raw)
	if err != nil {
		return
	}
	bh := b.Header.Hash()
	if n.chain.HasBlock(bh) {
		return
	}
	if err := n.chain.AcceptBlock(b); err != nil {
		if errors.Is(err, core.ErrOrphan) {
			n.addOrphan(bh, raw, b.Header.PrevHash, p)
			return
		}
		n.logf("p2p: rejected block %x… from %s: %v", bh[:8], p.name, err)
		return
	}
	n.logf("p2p: accepted block %d (%x…) from %s", b.Header.Height, bh[:8], p.name)
	n.broadcast(msgBlock, raw, p)
	n.connectOrphans(bh)
	n.maybeSync(p)
}

// addOrphan parks a block whose parent we lack and asks the sender for the
// parent, walking backwards until the branches join.
func (n *Node) addOrphan(bh [32]byte, raw []byte, parent [32]byte, p *peer) {
	n.mu.Lock()
	if len(n.orphans) >= maxOrphans {
		n.orphans = map[[32]byte][]byte{}
		n.waiting = map[[32]byte][][32]byte{}
	}
	if _, ok := n.orphans[bh]; !ok {
		n.orphans[bh] = append([]byte{}, raw...)
		n.waiting[parent] = append(n.waiting[parent], bh)
	}
	n.mu.Unlock()
	p.send(msgGetBlockByHash, parent[:])
}

// connectOrphans retries parked blocks whose missing ancestor just arrived,
// cascading through grandchildren.
func (n *Node) connectOrphans(parent [32]byte) {
	queue := [][32]byte{parent}
	for len(queue) > 0 {
		ph := queue[0]
		queue = queue[1:]
		n.mu.Lock()
		kids := n.waiting[ph]
		delete(n.waiting, ph)
		raws := make([][]byte, 0, len(kids))
		for _, kh := range kids {
			if raw, ok := n.orphans[kh]; ok {
				raws = append(raws, raw)
				delete(n.orphans, kh)
			}
		}
		n.mu.Unlock()
		for _, raw := range raws {
			b, err := core.DeserializeBlock(raw)
			if err != nil {
				continue
			}
			if err := n.chain.AcceptBlock(b); err == nil {
				bh := b.Header.Hash()
				n.broadcast(msgBlock, raw, nil)
				queue = append(queue, bh)
			}
		}
	}
}

func (n *Node) handleTx(p *peer, raw []byte) {
	t, err := core.DeserializeTx(raw)
	if err != nil {
		return
	}
	id := t.ID()
	n.mu.Lock()
	if _, seen := n.seenTx[id]; seen {
		n.mu.Unlock()
		return
	}
	if len(n.seenTx) >= maxSeenTx {
		n.seenTx = map[[32]byte]struct{}{}
	}
	n.seenTx[id] = struct{}{}
	n.mu.Unlock()
	if err := n.chain.SubmitTx(t); err != nil {
		return // invalid or duplicate: not relayed
	}
	n.broadcast(msgTx, raw, p)
}

// BroadcastBlock announces a locally mined block to all peers.
func (n *Node) BroadcastBlock(b *core.Block) {
	n.broadcast(msgBlock, b.Serialize(), nil)
}

// BroadcastTx announces a locally submitted transaction to all peers.
func (n *Node) BroadcastTx(t *core.Tx) {
	id := t.ID()
	n.mu.Lock()
	n.seenTx[id] = struct{}{}
	n.mu.Unlock()
	n.broadcast(msgTx, t.Serialize(), nil)
}
