package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ErrOrphan marks a block whose parent is unknown; the caller should fetch
// the parent and retry.
var ErrOrphan = errors.New("orphan block: parent unknown")

var (
	bBlocks  = []byte("blocks")
	bHeight  = []byte("height")
	bIndex   = []byte("index")
	bUndo    = []byte("undo")
	bUTXO    = []byte("utxo")
	bMeta    = []byte("meta")
	bMempool     = []byte("mempool")
	bMempoolMeta = []byte("mempoolmeta")  // txid -> fee, size, arrival
	bMempoolSpnt = []byte("mempoolspent") // outpoint -> txid holding it
	bTxIndex     = []byte("txindex")

	kTip       = []byte("tip")
	kTipHeight = []byte("tipheight")
	kTipWork   = []byte("tipwork")
	kPool      = []byte("pool")
	kEmitted   = []byte("emitted")
	kBurned    = []byte("burned")
)

var two256 = new(big.Int).Lsh(big.NewInt(1), 256)

// blockWork is the expected number of hash attempts a block's target
// represents; fork choice follows the chain with the most cumulative work.
func blockWork(target [32]byte) *big.Int {
	t := new(big.Int).SetBytes(target[:])
	t.Add(t, big.NewInt(1))
	return new(big.Int).Div(two256, t)
}

// idxEntry is the per-block index: height and cumulative chain work. Blocks on
// side branches are indexed too, so the node can evaluate competing chains.
type idxEntry struct {
	Height uint64
	Work   *big.Int
}

func (e *idxEntry) serialize() []byte {
	out := make([]byte, 40)
	binary.BigEndian.PutUint64(out[:8], e.Height)
	e.Work.FillBytes(out[8:40])
	return out
}

func readIdx(dbtx *bolt.Tx, hash []byte) *idxEntry {
	v := dbtx.Bucket(bIndex).Get(hash)
	if len(v) != 40 {
		return nil
	}
	return &idxEntry{
		Height: binary.BigEndian.Uint64(v[:8]),
		Work:   new(big.Int).SetBytes(v[8:40]),
	}
}

type UTXO struct {
	Value    uint64
	Addr     [32]byte
	Height   uint64
	Coinbase bool
}

func (u *UTXO) serialize() []byte {
	w := &buf{}
	w.u64(u.Value)
	w.raw(u.Addr[:])
	w.u64(u.Height)
	if u.Coinbase {
		w.raw([]byte{1})
	} else {
		w.raw([]byte{0})
	}
	return w.b
}

func deserializeUTXO(b []byte) (*UTXO, error) {
	r := &rdr{b: b}
	u := &UTXO{}
	u.Value = r.u64()
	u.Addr = r.arr32()
	u.Height = r.u64()
	f := r.take(1)
	if err := r.done(); err != nil {
		return nil, err
	}
	u.Coinbase = f[0] == 1
	return u, nil
}

func utxoKey(op OutPoint) []byte {
	k := make([]byte, 36)
	copy(k, op.TxID[:])
	binary.BigEndian.PutUint32(k[32:], op.Index)
	return k
}

// undoRec captures everything needed to disconnect a block during a reorg:
// the UTXOs it spent and the supply accounting before it connected.
type undoRec struct {
	spentKeys [][]byte
	spentVals [][]byte
	pool      uint64
	emitted   uint64
	burned    uint64
}

func (u *undoRec) serialize() []byte {
	w := &buf{}
	w.u32(uint32(len(u.spentKeys)))
	for i := range u.spentKeys {
		w.raw(u.spentKeys[i])
		w.raw(u.spentVals[i])
	}
	w.u64(u.pool)
	w.u64(u.emitted)
	w.u64(u.burned)
	return w.b
}

func parseUndo(b []byte) (*undoRec, error) {
	r := &rdr{b: b}
	u := &undoRec{}
	n := int(r.u32())
	if n > 1_000_000 {
		return nil, errTruncated
	}
	for i := 0; i < n; i++ {
		u.spentKeys = append(u.spentKeys, r.take(36))
		u.spentVals = append(u.spentVals, r.take(49))
	}
	u.pool = r.u64()
	u.emitted = r.u64()
	u.burned = r.u64()
	if err := r.done(); err != nil {
		return nil, err
	}
	return u, nil
}

// Chain is the on-disk blockchain state: all known blocks (including side
// branches), the active chain's UTXO set, supply accounting and the local
// mempool. Fork choice: most cumulative work wins; competing branches are
// reorganized atomically.
type Chain struct {
	db    *bolt.DB
	epoch atomic.Uint64 // bumped whenever the active tip changes
}

func Open(path string) (*Chain, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		return nil, err
	}
	c := &Chain{db: db}
	err = db.Update(func(dbtx *bolt.Tx) error {
		for _, name := range [][]byte{bBlocks, bHeight, bIndex, bUndo, bUTXO, bMeta, bMempool, bMempoolMeta, bMempoolSpnt, bTxIndex} {
			if _, err := dbtx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		meta := dbtx.Bucket(bMeta)
		if tip := meta.Get(kTip); tip != nil {
			if dbtx.Bucket(bIndex).Get(tip) == nil {
				if err := rebuildIndex(dbtx); err != nil {
					return err
				}
			}
			// Databases written before the transaction index existed carry an
			// empty bucket; fill it once rather than degrading lookups forever.
			if dbtx.Bucket(bTxIndex).Stats().KeyN == 0 && getU64(meta, kTipHeight) > 0 {
				return rebuildTxIndex(dbtx)
			}
			return nil
		}
		g := GenesisBlock()
		gh := g.Header.Hash()
		if err := dbtx.Bucket(bBlocks).Put(gh[:], g.Serialize()); err != nil {
			return err
		}
		if err := dbtx.Bucket(bHeight).Put(be64(0), gh[:]); err != nil {
			return err
		}
		gw := blockWork(g.Header.Target)
		e := &idxEntry{Height: 0, Work: gw}
		if err := dbtx.Bucket(bIndex).Put(gh[:], e.serialize()); err != nil {
			return err
		}
		if err := meta.Put(kTip, gh[:]); err != nil {
			return err
		}
		var wb [32]byte
		gw.FillBytes(wb[:])
		if err := meta.Put(kTipWork, wb[:]); err != nil {
			return err
		}
		for _, k := range [][]byte{kTipHeight, kPool, kEmitted, kBurned} {
			if err := meta.Put(k, be64(0)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return c, nil
}

// rebuildIndex migrates databases created before fork choice existed: it
// derives the block index from the active chain. Pre-migration blocks carry
// no undo data, so reorgs below the migration point are refused.
func rebuildIndex(dbtx *bolt.Tx) error {
	meta := dbtx.Bucket(bMeta)
	tipHeight := getU64(meta, kTipHeight)
	work := new(big.Int)
	for h := uint64(0); h <= tipHeight; h++ {
		hash := dbtx.Bucket(bHeight).Get(be64(h))
		if hash == nil {
			return fmt.Errorf("corrupt chain: missing height %d", h)
		}
		hdr, err := readHeaderByHash(dbtx, hash)
		if err != nil {
			return err
		}
		work.Add(work, blockWork(hdr.Target))
		e := &idxEntry{Height: h, Work: new(big.Int).Set(work)}
		if err := dbtx.Bucket(bIndex).Put(hash, e.serialize()); err != nil {
			return err
		}
	}
	var wb [32]byte
	work.FillBytes(wb[:])
	return meta.Put(kTipWork, wb[:])
}

// rebuildTxIndex walks the active chain and records where every transaction
// lives. Run once, when opening a database created before the index existed.
func rebuildTxIndex(dbtx *bolt.Tx) error {
	tipHeight := getU64(dbtx.Bucket(bMeta), kTipHeight)
	idx := dbtx.Bucket(bTxIndex)
	for h := uint64(0); h <= tipHeight; h++ {
		hash := dbtx.Bucket(bHeight).Get(be64(h))
		if hash == nil {
			continue
		}
		b, err := readBlock(dbtx, hash)
		if err != nil {
			return err
		}
		for _, t := range b.Txs {
			id := t.ID()
			if err := idx.Put(id[:], be64(h)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Chain) Close() error { return c.db.Close() }

// TipEpoch increments every time the active tip changes; miners use it to
// abandon stale work immediately.
func (c *Chain) TipEpoch() uint64 { return c.epoch.Load() }

// GenesisBlock is the hardcoded block 0. It carries no transactions and no
// reward — the fair-launch anchor of the timeline. Its merkle root commits to
// GenesisMessage instead of a transaction list.
func GenesisBlock() *Block {
	return &Block{Header: BlockHeader{
		Version:    1,
		Height:     0,
		Time:       GenesisTime,
		MerkleRoot: H([]byte("PER:genesis"), []byte(GenesisMessage)),
		Target:     TargetToBytes(InitialTarget()),
	}}
}

func getU64(b *bolt.Bucket, k []byte) uint64 {
	v := b.Get(k)
	if len(v) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(v)
}

func readBlock(dbtx *bolt.Tx, hash []byte) (*Block, error) {
	raw := dbtx.Bucket(bBlocks).Get(hash)
	if raw == nil {
		return nil, fmt.Errorf("block %x not found", hash)
	}
	return DeserializeBlock(raw)
}

func readHeaderByHash(dbtx *bolt.Tx, hash []byte) (*BlockHeader, error) {
	raw := dbtx.Bucket(bBlocks).Get(hash)
	if raw == nil {
		return nil, fmt.Errorf("block %x not found", hash)
	}
	return DeserializeHeader(raw)
}

func readHeaderAt(dbtx *bolt.Tx, height uint64) (*BlockHeader, error) {
	hash := dbtx.Bucket(bHeight).Get(be64(height))
	if hash == nil {
		return nil, fmt.Errorf("no block at height %d", height)
	}
	return readHeaderByHash(dbtx, hash)
}

// headersBack collects up to count consecutive headers ending at (and
// including) the block `from`, oldest first. Works on any branch.
func headersBack(dbtx *bolt.Tx, from [32]byte, count int) ([]*BlockHeader, error) {
	var rev []*BlockHeader
	h := from
	for len(rev) < count {
		hd, err := readHeaderByHash(dbtx, h[:])
		if err != nil {
			return nil, err
		}
		rev = append(rev, hd)
		if hd.Height == 0 {
			break
		}
		h = hd.PrevHash
	}
	out := make([]*BlockHeader, len(rev))
	for i := range rev {
		out[len(rev)-1-i] = rev[i]
	}
	return out, nil
}

// checkTx validates a non-coinbase transaction (existence, maturity, values,
// signatures) against the UTXO set and returns its fee. It reads the spent map
// to reject conflicts but never writes it; the caller records accepted spends.
func checkTx(dbtx *bolt.Tx, t *Tx, height uint64, spent map[OutPoint]bool) (uint64, error) {
	if len(t.Inputs) == 0 || len(t.Inputs) > 1_000 {
		return 0, fmt.Errorf("bad input count")
	}
	if len(t.Outputs) == 0 || len(t.Outputs) > 1_000 {
		return 0, fmt.Errorf("bad output count")
	}
	if len(t.Extra) > MaxTxExtra {
		return 0, fmt.Errorf("extra data too large")
	}
	utxos := dbtx.Bucket(bUTXO)
	var inSum, outSum uint64
	// A transaction must not list the same output twice. Without this the
	// value of a single unspent output would be counted once per mention,
	// letting anyone holding one coin claim several — coins created from
	// nothing, with no hashpower required. The caller's `spent` map guards
	// against conflicts with *other* transactions and cannot cover this.
	seen := make(map[OutPoint]struct{}, len(t.Inputs))
	for i := range t.Inputs {
		op := t.Inputs[i].Prev
		if _, dup := seen[op]; dup {
			return 0, fmt.Errorf("input %d: output already spent by input of the same transaction", i)
		}
		seen[op] = struct{}{}
		if spent[op] {
			return 0, fmt.Errorf("input %d: double spend", i)
		}
		raw := utxos.Get(utxoKey(op))
		if raw == nil {
			return 0, fmt.Errorf("input %d: unknown or spent output", i)
		}
		u, err := deserializeUTXO(raw)
		if err != nil {
			return 0, err
		}
		if u.Coinbase && height-u.Height < CoinbaseMaturity {
			return 0, fmt.Errorf("input %d: coinbase not yet mature", i)
		}
		inSum += u.Value
	}
	for i := range t.Outputs {
		if t.Outputs[i].Value > MaxSupply {
			return 0, fmt.Errorf("output %d: value out of range", i)
		}
		outSum += t.Outputs[i].Value
	}
	if outSum > inSum {
		return 0, fmt.Errorf("outputs (%s) exceed inputs (%s)", FormatAmount(outSum), FormatAmount(inSum))
	}
	if err := t.VerifySignatures(func(op OutPoint) ([32]byte, bool) {
		raw := utxos.Get(utxoKey(op))
		if raw == nil {
			return [32]byte{}, false
		}
		u, err := deserializeUTXO(raw)
		if err != nil {
			return [32]byte{}, false
		}
		return u.Addr, true
	}); err != nil {
		return 0, err
	}
	return inSum - outSum, nil
}

// checkHeaderContextual runs every validation that does not need UTXO state:
// linkage, timestamps, difficulty, structure, merkle commitment and PoW.
func checkHeaderContextual(dbtx *bolt.Tx, b *Block, parent *BlockHeader) error {
	hdr := &b.Header
	if !ValidBlockVersion(hdr.Version) {
		return fmt.Errorf("malformed block version %#x", hdr.Version)
	}
	if hdr.Height != parent.Height+1 {
		return fmt.Errorf("height %d does not follow parent height %d", hdr.Height, parent.Height)
	}
	if hdr.Time < parent.Time-TargetBlockTime {
		return fmt.Errorf("timestamp too far in the past")
	}
	if hdr.Time > time.Now().Unix()+MaxFutureDrift {
		return fmt.Errorf("timestamp too far in the future")
	}
	hdrs, err := headersBack(dbtx, hdr.PrevHash, DifficultyWindow+1)
	if err != nil {
		return err
	}
	if NextTarget(hdrs) != hdr.Target {
		return fmt.Errorf("wrong difficulty target")
	}
	if len(b.Txs) == 0 || !b.Txs[0].IsCoinbase() {
		return fmt.Errorf("first transaction must be coinbase")
	}
	cb := b.Txs[0]
	if !bytes.Equal(cb.Extra, CoinbaseExtra(hdr.Height)) {
		return fmt.Errorf("coinbase must commit to height")
	}
	if len(cb.Outputs) == 0 || len(cb.Outputs) > 100 {
		return fmt.Errorf("bad coinbase outputs")
	}
	for i, t := range b.Txs[1:] {
		if t.IsCoinbase() {
			return fmt.Errorf("tx %d: unexpected coinbase", i+1)
		}
	}
	seen := map[[32]byte]bool{}
	for _, t := range b.Txs {
		id := t.ID()
		if seen[id] {
			return fmt.Errorf("duplicate transaction in block")
		}
		seen[id] = true
	}
	if MerkleRoot(b.Txs) != hdr.MerkleRoot {
		return fmt.Errorf("merkle root mismatch")
	}
	if !CheckPow(hdr) {
		return fmt.Errorf("invalid proof of work")
	}
	return nil
}

// connectTip applies a block whose parent is the current active tip: full
// transaction validation, UTXO updates, undo capture and supply accounting.
func connectTip(dbtx *bolt.Tx, b *Block, bh [32]byte) error {
	meta := dbtx.Bucket(bMeta)
	height := b.Header.Height

	spent := map[OutPoint]bool{}
	var burnAdd, poolAdd uint64
	for i, t := range b.Txs[1:] {
		fee, err := checkTx(dbtx, t, height, spent)
		if err != nil {
			return fmt.Errorf("tx %d: %w", i+1, err)
		}
		for j := range t.Inputs {
			spent[t.Inputs[j].Prev] = true
		}
		bn, pl := SplitFee(fee)
		burnAdd += bn
		poolAdd += pl
	}
	poolBal := getU64(meta, kPool)
	payout := PoolPayout(poolBal + poolAdd)
	subsidy := BlockSubsidy(height)
	cb := b.Txs[0]
	var cbOut uint64
	for i := range cb.Outputs {
		if cb.Outputs[i].Value > MaxSupply {
			return fmt.Errorf("coinbase output out of range")
		}
		cbOut += cb.Outputs[i].Value
	}
	if cbOut != subsidy+payout {
		return fmt.Errorf("coinbase pays %s, expected %s PER", FormatAmount(cbOut), FormatAmount(subsidy+payout))
	}

	utxos := dbtx.Bucket(bUTXO)
	undo := &undoRec{
		pool:    poolBal,
		emitted: getU64(meta, kEmitted),
		burned:  getU64(meta, kBurned),
	}
	for _, t := range b.Txs[1:] {
		for i := range t.Inputs {
			key := utxoKey(t.Inputs[i].Prev)
			val := utxos.Get(key)
			undo.spentKeys = append(undo.spentKeys, key)
			undo.spentVals = append(undo.spentVals, append([]byte{}, val...))
			if err := utxos.Delete(key); err != nil {
				return err
			}
		}
	}
	txIndex := dbtx.Bucket(bTxIndex)
	for _, t := range b.Txs {
		id := t.ID()
		for i := range t.Outputs {
			u := &UTXO{
				Value:    t.Outputs[i].Value,
				Addr:     t.Outputs[i].Addr,
				Height:   height,
				Coinbase: t.IsCoinbase(),
			}
			if err := utxos.Put(utxoKey(OutPoint{TxID: id, Index: uint32(i)}), u.serialize()); err != nil {
				return err
			}
		}
		// Record where each transaction lives, so looking one up is a single
		// read rather than a scan over recent blocks.
		if err := txIndex.Put(id[:], be64(height)); err != nil {
			return err
		}
	}
	if err := dbtx.Bucket(bUndo).Put(bh[:], undo.serialize()); err != nil {
		return err
	}
	if err := dbtx.Bucket(bHeight).Put(be64(height), bh[:]); err != nil {
		return err
	}
	e := readIdx(dbtx, bh[:])
	if e == nil {
		return fmt.Errorf("missing index entry for connecting block")
	}
	var wb [32]byte
	e.Work.FillBytes(wb[:])
	if err := meta.Put(kTip, bh[:]); err != nil {
		return err
	}
	if err := meta.Put(kTipHeight, be64(height)); err != nil {
		return err
	}
	if err := meta.Put(kTipWork, wb[:]); err != nil {
		return err
	}
	newEmitted := undo.emitted + subsidy
	if newEmitted > MaxSupply {
		return fmt.Errorf("emission exceeds max supply — consensus bug")
	}
	if err := meta.Put(kEmitted, be64(newEmitted)); err != nil {
		return err
	}
	if err := meta.Put(kBurned, be64(undo.burned+burnAdd)); err != nil {
		return err
	}
	if err := meta.Put(kPool, be64(poolBal+poolAdd-payout)); err != nil {
		return err
	}

	// Prune the mempool: drop anything whose inputs are no longer unspent
	// (removes both included and now-conflicting transactions).
	mp := dbtx.Bucket(bMempool)
	var drop [][]byte
	err := mp.ForEach(func(k, v []byte) error {
		t, err := DeserializeTx(v)
		if err != nil {
			drop = append(drop, append([]byte{}, k...))
			return nil
		}
		for i := range t.Inputs {
			if utxos.Get(utxoKey(t.Inputs[i].Prev)) == nil {
				drop = append(drop, append([]byte{}, k...))
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range drop {
		var id [32]byte
		copy(id[:], k)
		if err := removeFromMempool(dbtx, id); err != nil {
			return err
		}
	}
	return nil
}

// disconnectTip unwinds the active tip using its undo record and returns its
// non-coinbase transactions to the mempool.
func disconnectTip(dbtx *bolt.Tx) error {
	meta := dbtx.Bucket(bMeta)
	tipHeight := getU64(meta, kTipHeight)
	if tipHeight == 0 {
		return fmt.Errorf("cannot disconnect genesis")
	}
	tipHash := append([]byte{}, meta.Get(kTip)...)
	b, err := readBlock(dbtx, tipHash)
	if err != nil {
		return err
	}
	undoRaw := dbtx.Bucket(bUndo).Get(tipHash)
	if undoRaw == nil {
		return fmt.Errorf("no undo data for block %x (pre-upgrade chain)", tipHash[:8])
	}
	u, err := parseUndo(undoRaw)
	if err != nil {
		return err
	}
	utxos := dbtx.Bucket(bUTXO)
	txIndex := dbtx.Bucket(bTxIndex)
	for _, t := range b.Txs {
		id := t.ID()
		for i := range t.Outputs {
			if err := utxos.Delete(utxoKey(OutPoint{TxID: id, Index: uint32(i)})); err != nil {
				return err
			}
		}
		if err := txIndex.Delete(id[:]); err != nil {
			return err
		}
	}
	for i := range u.spentKeys {
		if err := utxos.Put(u.spentKeys[i], u.spentVals[i]); err != nil {
			return err
		}
	}
	if err := meta.Put(kPool, be64(u.pool)); err != nil {
		return err
	}
	if err := meta.Put(kEmitted, be64(u.emitted)); err != nil {
		return err
	}
	if err := meta.Put(kBurned, be64(u.burned)); err != nil {
		return err
	}
	if err := dbtx.Bucket(bUndo).Delete(tipHash); err != nil {
		return err
	}
	if err := dbtx.Bucket(bHeight).Delete(be64(tipHeight)); err != nil {
		return err
	}
	parent := b.Header.PrevHash
	pe := readIdx(dbtx, parent[:])
	if pe == nil {
		return fmt.Errorf("corrupt chain: parent of tip not indexed")
	}
	var wb [32]byte
	pe.Work.FillBytes(wb[:])
	if err := meta.Put(kTip, parent[:]); err != nil {
		return err
	}
	if err := meta.Put(kTipHeight, be64(tipHeight-1)); err != nil {
		return err
	}
	if err := meta.Put(kTipWork, wb[:]); err != nil {
		return err
	}
	// Transactions from the disconnected block return to the pool. Their
	// inputs were just restored to the UTXO set above, so the fee can be
	// recomputed here and the pool indexes stay consistent.
	for _, t := range b.Txs[1:] {
		id := t.ID()
		raw := t.Serialize()
		var in, out uint64
		ok := true
		for i := range t.Inputs {
			v := utxos.Get(utxoKey(t.Inputs[i].Prev))
			if v == nil {
				ok = false
				break
			}
			u, err := deserializeUTXO(v)
			if err != nil {
				ok = false
				break
			}
			in += u.Value
		}
		for i := range t.Outputs {
			out += t.Outputs[i].Value
		}
		if !ok || in < out {
			continue // cannot be reinstated coherently; drop rather than guess
		}
		if err := addToMempool(dbtx, id, raw, t, in-out); err != nil {
			return err
		}
	}
	return nil
}

// reorgTo switches the active chain to the branch ending in b. Runs inside
// the caller's transaction: any failure rolls back everything, leaving the
// previous chain untouched.
func reorgTo(dbtx *bolt.Tx, b *Block) error {
	meta := dbtx.Bucket(bMeta)
	heights := dbtx.Bucket(bHeight)

	branch := []*Block{b}
	cur := &b.Header
	for {
		ph := cur.PrevHash
		pe := readIdx(dbtx, ph[:])
		if pe == nil {
			return fmt.Errorf("missing ancestor during reorg")
		}
		if bytes.Equal(heights.Get(be64(pe.Height)), ph[:]) {
			break // parent is on the active chain: fork point found
		}
		pb, err := readBlock(dbtx, ph[:])
		if err != nil {
			return err
		}
		branch = append([]*Block{pb}, branch...)
		cur = &pb.Header
	}
	forkHeight := branch[0].Header.Height - 1
	for getU64(meta, kTipHeight) > forkHeight {
		if err := disconnectTip(dbtx); err != nil {
			return err
		}
	}
	for _, blk := range branch {
		if err := connectTip(dbtx, blk, blk.Header.Hash()); err != nil {
			return fmt.Errorf("reorg aborted, chain unchanged: %w", err)
		}
	}
	return nil
}

// AcceptBlock validates and stores a block. If it extends the active tip it
// is connected directly; if it completes a branch with more cumulative work,
// the chain reorganizes to it atomically; otherwise it is kept as a side
// branch. Returns ErrOrphan when the parent is unknown.
func (c *Chain) AcceptBlock(b *Block) error {
	raw := b.Serialize()
	if len(raw) > MaxBlockBytes {
		return fmt.Errorf("block too large")
	}
	bh := b.Header.Hash()
	var tipChanged bool
	err := c.db.Update(func(dbtx *bolt.Tx) error {
		if readIdx(dbtx, bh[:]) != nil {
			return nil // duplicate
		}
		pe := readIdx(dbtx, b.Header.PrevHash[:])
		if pe == nil {
			return ErrOrphan
		}
		parentHdr, err := readHeaderByHash(dbtx, b.Header.PrevHash[:])
		if err != nil {
			return err
		}
		if err := checkHeaderContextual(dbtx, b, parentHdr); err != nil {
			return err
		}
		work := new(big.Int).Add(pe.Work, blockWork(b.Header.Target))
		if err := dbtx.Bucket(bBlocks).Put(bh[:], raw); err != nil {
			return err
		}
		e := &idxEntry{Height: b.Header.Height, Work: work}
		if err := dbtx.Bucket(bIndex).Put(bh[:], e.serialize()); err != nil {
			return err
		}
		meta := dbtx.Bucket(bMeta)
		if bytes.Equal(b.Header.PrevHash[:], meta.Get(kTip)) {
			if err := connectTip(dbtx, b, bh); err != nil {
				return err
			}
			tipChanged = true
			return nil
		}
		tipWork := new(big.Int).SetBytes(meta.Get(kTipWork))
		if work.Cmp(tipWork) > 0 {
			if err := reorgTo(dbtx, b); err != nil {
				return err
			}
			tipChanged = true
		}
		return nil
	})
	if err == nil && tipChanged {
		c.epoch.Add(1)
	}
	return err
}

// AddBlock is a compatibility alias for AcceptBlock.
func (c *Chain) AddBlock(b *Block) error { return c.AcceptBlock(b) }

// SubmitTx validates a transaction against the confirmed UTXO set and pending
// mempool spends, then queues it for inclusion in the next mined block.
func (c *Chain) SubmitTx(t *Tx) error {
	raw := t.Serialize()
	if len(raw) > MaxTxBytes {
		return fmt.Errorf("transaction too large (%d bytes)", len(raw))
	}
	if t.IsCoinbase() {
		return fmt.Errorf("refusing standalone coinbase")
	}
	id := t.ID()
	return c.db.Update(func(dbtx *bolt.Tx) error {
		mp := dbtx.Bucket(bMempool)
		if mp.Get(id[:]) != nil {
			return fmt.Errorf("transaction already pending")
		}
		// Conflicts are found by index rather than by walking the pool, so
		// admission costs the same whether the pool holds ten transactions or
		// thousands. The previous full scan made flooding quadratically
		// expensive for the victim and linear for the attacker.
		spent := dbtx.Bucket(bMempoolSpnt)
		for i := range t.Inputs {
			if holder := spent.Get(utxoKey(t.Inputs[i].Prev)); holder != nil {
				return fmt.Errorf("input %d conflicts with a pending transaction", i)
			}
		}
		next := getU64(dbtx.Bucket(bMeta), kTipHeight) + 1
		fee, err := checkTx(dbtx, t, next, nil)
		if err != nil {
			return err
		}
		if fee < MinRelayFee {
			return fmt.Errorf("fee %s below minimum %s PER", FormatAmount(fee), FormatAmount(MinRelayFee))
		}

		// Enforce the pool's capacity. When it is full a newcomer must pay a
		// better rate than the worst resident, which it then displaces; this
		// makes filling the pool cost the attacker progressively more instead
		// of nothing.
		rate := feeRate(fee, len(raw))
		st := mp.Stats()
		if st.KeyN >= MaxMempoolTxs || st.LeafInuse+len(raw) > MaxMempoolBytes {
			worstID, worstRate, ok := worstInPool(dbtx)
			if !ok || rate <= worstRate {
				return fmt.Errorf("mempool is full and this fee rate does not displace anything")
			}
			if err := removeFromMempool(dbtx, worstID); err != nil {
				return err
			}
		}
		return addToMempool(dbtx, id, raw, t, fee)
	})
}

// feeRate is fee per byte, scaled so that integer division keeps resolution.
func feeRate(fee uint64, size int) uint64 {
	if size <= 0 {
		return 0
	}
	return fee * 1000 / uint64(size)
}

func mempoolMeta(fee uint64, size int) []byte {
	w := &buf{}
	w.u64(fee)
	w.u32(uint32(size))
	return w.b
}

func parseMempoolMeta(b []byte) (fee uint64, size int, ok bool) {
	if len(b) != 12 {
		return 0, 0, false
	}
	r := &rdr{b: b}
	fee = r.u64()
	size = int(r.u32())
	return fee, size, r.done() == nil
}

func addToMempool(dbtx *bolt.Tx, id [32]byte, raw []byte, t *Tx, fee uint64) error {
	if err := dbtx.Bucket(bMempool).Put(id[:], raw); err != nil {
		return err
	}
	if err := dbtx.Bucket(bMempoolMeta).Put(id[:], mempoolMeta(fee, len(raw))); err != nil {
		return err
	}
	spent := dbtx.Bucket(bMempoolSpnt)
	for i := range t.Inputs {
		if err := spent.Put(utxoKey(t.Inputs[i].Prev), id[:]); err != nil {
			return err
		}
	}
	return nil
}

func removeFromMempool(dbtx *bolt.Tx, id [32]byte) error {
	mp := dbtx.Bucket(bMempool)
	if raw := mp.Get(id[:]); raw != nil {
		if t, err := DeserializeTx(raw); err == nil {
			spent := dbtx.Bucket(bMempoolSpnt)
			for i := range t.Inputs {
				key := utxoKey(t.Inputs[i].Prev)
				// Only clear the reservation if it is still ours.
				if h := spent.Get(key); h != nil && [32]byte(h) == id {
					if err := spent.Delete(key); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := mp.Delete(id[:]); err != nil {
		return err
	}
	return dbtx.Bucket(bMempoolMeta).Delete(id[:])
}

// worstInPool finds the resident transaction paying the lowest fee rate.
func worstInPool(dbtx *bolt.Tx) ([32]byte, uint64, bool) {
	var worstID [32]byte
	worstRate := ^uint64(0)
	found := false
	dbtx.Bucket(bMempoolMeta).ForEach(func(k, v []byte) error {
		fee, size, ok := parseMempoolMeta(v)
		if !ok || len(k) != 32 {
			return nil
		}
		if r := feeRate(fee, size); r < worstRate {
			worstRate = r
			copy(worstID[:], k)
			found = true
		}
		return nil
	})
	return worstID, worstRate, found
}

// NextBlockTemplate assembles an unsolved block on top of the tip: coinbase
// paying subsidy + pool payout to the miner, plus valid mempool transactions.
func (c *Chain) NextBlockTemplate(miner [32]byte) (*Block, error) {
	var blk *Block
	err := c.db.View(func(dbtx *bolt.Tx) error {
		meta := dbtx.Bucket(bMeta)
		tipHeight := getU64(meta, kTipHeight)
		var prev [32]byte
		copy(prev[:], meta.Get(kTip))
		parent, err := readHeaderAt(dbtx, tipHeight)
		if err != nil {
			return err
		}
		height := tipHeight + 1

		var sel []*Tx
		var poolAdd uint64
		spent := map[OutPoint]bool{}
		used := 0
		budget := MaxBlockBytes - 32*1024 // slack for header + coinbase
		err = dbtx.Bucket(bMempool).ForEach(func(k, v []byte) error {
			if used+len(v)+4 > budget {
				return nil
			}
			t, err := DeserializeTx(v)
			if err != nil {
				return nil // skip malformed
			}
			fee, err := checkTx(dbtx, t, height, spent)
			if err != nil {
				return nil // skip currently-invalid
			}
			for i := range t.Inputs {
				spent[t.Inputs[i].Prev] = true
			}
			_, pl := SplitFee(fee)
			poolAdd += pl
			sel = append(sel, t)
			used += len(v) + 4
			return nil
		})
		if err != nil {
			return err
		}

		poolBal := getU64(meta, kPool)
		payout := PoolPayout(poolBal + poolAdd)
		subsidy := BlockSubsidy(height)
		cb := &Tx{
			Outputs: []TxOutput{{Value: subsidy + payout, Addr: miner}},
			Extra:   CoinbaseExtra(height),
		}
		txs := append([]*Tx{cb}, sel...)

		now := time.Now().Unix()
		if now < parent.Time {
			now = parent.Time
		}
		hdrs, err := headersBack(dbtx, prev, DifficultyWindow+1)
		if err != nil {
			return err
		}
		blk = &Block{
			Header: BlockHeader{
				Version:    VersionSignalBase | (minerSignalBits & VersionBitsMask),
				Height:     height,
				PrevHash:   prev,
				MerkleRoot: MerkleRoot(txs),
				Time:       now,
				Target:     NextTarget(hdrs),
			},
			Txs: txs,
		}
		return nil
	})
	return blk, err
}

func (c *Chain) BlockByHeight(h uint64) (*Block, error) {
	var blk *Block
	err := c.db.View(func(dbtx *bolt.Tx) error {
		hash := dbtx.Bucket(bHeight).Get(be64(h))
		if hash == nil {
			return fmt.Errorf("no block at height %d", h)
		}
		b, err := readBlock(dbtx, hash)
		blk = b
		return err
	})
	return blk, err
}

// TxByID returns a confirmed transaction and the height of the block holding
// it. It is an index lookup, so it costs the same whether the transaction is
// recent or ancient — which is what keeps a public lookup service from being
// a denial-of-service lever.
func (c *Chain) TxByID(id [32]byte) (*Tx, uint64, error) {
	var (
		tx     *Tx
		height uint64
	)
	err := c.db.View(func(dbtx *bolt.Tx) error {
		v := dbtx.Bucket(bTxIndex).Get(id[:])
		if v == nil {
			return fmt.Errorf("unknown transaction")
		}
		height = binary.BigEndian.Uint64(v)
		hash := dbtx.Bucket(bHeight).Get(be64(height))
		if hash == nil {
			return fmt.Errorf("transaction indexed at a height that is no longer active")
		}
		b, err := readBlock(dbtx, hash)
		if err != nil {
			return err
		}
		for _, t := range b.Txs {
			if t.ID() == id {
				tx = t
				return nil
			}
		}
		return fmt.Errorf("transaction not present in its indexed block")
	})
	if err != nil {
		return nil, 0, err
	}
	return tx, height, nil
}

// SupplyAudit is the arithmetic that must hold if no coin was ever created
// outside the rules.
type SupplyAudit struct {
	Emitted    uint64 // total block subsidies ever paid
	Burned     uint64 // fees destroyed
	Pool       uint64 // fees credited but not yet paid out
	UTXOTotal  uint64 // value actually present in unspent outputs
	Expected   uint64 // Emitted − Burned − Pool
	Outputs    int
	Consistent bool
}

// AuditSupply totals the unspent output set and compares it with the supply
// the chain claims to have issued. Every coin that exists must be either an
// unspent output or still sitting in the reward pool; anything else means
// value was created outside consensus. This is the check that reveals an
// inflation bug after the fact, so it is worth running on any node whose
// history predates a consensus fix.
func (c *Chain) AuditSupply() (*SupplyAudit, error) {
	a := &SupplyAudit{}
	err := c.db.View(func(dbtx *bolt.Tx) error {
		meta := dbtx.Bucket(bMeta)
		a.Emitted = getU64(meta, kEmitted)
		a.Burned = getU64(meta, kBurned)
		a.Pool = getU64(meta, kPool)
		return dbtx.Bucket(bUTXO).ForEach(func(k, v []byte) error {
			u, err := deserializeUTXO(v)
			if err != nil {
				return err
			}
			a.UTXOTotal += u.Value
			a.Outputs++
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	a.Expected = a.Emitted - a.Burned - a.Pool
	a.Consistent = a.UTXOTotal == a.Expected
	return a, nil
}

// TipInfo returns the active tip's height and hash.
func (c *Chain) TipInfo() (uint64, [32]byte, error) {
	var height uint64
	var hash [32]byte
	err := c.db.View(func(dbtx *bolt.Tx) error {
		meta := dbtx.Bucket(bMeta)
		height = getU64(meta, kTipHeight)
		copy(hash[:], meta.Get(kTip))
		return nil
	})
	return height, hash, err
}

func (c *Chain) HasBlock(h [32]byte) bool {
	var ok bool
	c.db.View(func(dbtx *bolt.Tx) error {
		ok = dbtx.Bucket(bBlocks).Get(h[:]) != nil
		return nil
	})
	return ok
}

func (c *Chain) RawBlockByHash(h [32]byte) ([]byte, bool) {
	var out []byte
	c.db.View(func(dbtx *bolt.Tx) error {
		if v := dbtx.Bucket(bBlocks).Get(h[:]); v != nil {
			out = append([]byte{}, v...)
		}
		return nil
	})
	return out, out != nil
}

// RawBlocksFrom returns up to count consecutive active-chain blocks starting
// at the given height (capped at 500 per request).
func (c *Chain) RawBlocksFrom(from uint64, count int) ([][]byte, error) {
	if count > 500 {
		count = 500
	}
	if count < 1 {
		count = 1
	}
	var out [][]byte
	err := c.db.View(func(dbtx *bolt.Tx) error {
		heights := dbtx.Bucket(bHeight)
		blocks := dbtx.Bucket(bBlocks)
		for h := from; h < from+uint64(count); h++ {
			hash := heights.Get(be64(h))
			if hash == nil {
				break
			}
			raw := blocks.Get(hash)
			if raw == nil {
				break
			}
			out = append(out, append([]byte{}, raw...))
		}
		return nil
	})
	return out, err
}

// MempoolRaw returns up to 100 pending transactions for gossip.
func (c *Chain) MempoolRaw() [][]byte {
	var out [][]byte
	c.db.View(func(dbtx *bolt.Tx) error {
		return dbtx.Bucket(bMempool).ForEach(func(k, v []byte) error {
			if len(out) < 100 {
				out = append(out, append([]byte{}, v...))
			}
			return nil
		})
	})
	return out
}

// Balance sums the UTXOs of an address, split into spendable and immature.
func (c *Chain) Balance(addr [32]byte) (spendable, immature uint64, err error) {
	err = c.db.View(func(dbtx *bolt.Tx) error {
		next := getU64(dbtx.Bucket(bMeta), kTipHeight) + 1
		return dbtx.Bucket(bUTXO).ForEach(func(k, v []byte) error {
			u, err := deserializeUTXO(v)
			if err != nil {
				return err
			}
			if u.Addr != addr {
				return nil
			}
			if u.Coinbase && next-u.Height < CoinbaseMaturity {
				immature += u.Value
			} else {
				spendable += u.Value
			}
			return nil
		})
	})
	return
}

type Spendable struct {
	Out   OutPoint
	Value uint64
}

// ListSpendable returns mature UTXOs for addr totalling at least want,
// largest first (or all of them if want == 0).
func (c *Chain) ListSpendable(addr [32]byte, want uint64) ([]Spendable, error) {
	var all []Spendable
	err := c.db.View(func(dbtx *bolt.Tx) error {
		next := getU64(dbtx.Bucket(bMeta), kTipHeight) + 1
		return dbtx.Bucket(bUTXO).ForEach(func(k, v []byte) error {
			u, err := deserializeUTXO(v)
			if err != nil {
				return err
			}
			if u.Addr != addr {
				return nil
			}
			if u.Coinbase && next-u.Height < CoinbaseMaturity {
				return nil
			}
			var op OutPoint
			copy(op.TxID[:], k[:32])
			op.Index = binary.BigEndian.Uint32(k[32:])
			all = append(all, Spendable{Out: op, Value: u.Value})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Value > all[j].Value })
	if want == 0 {
		return all, nil
	}
	var sum uint64
	for i := range all {
		sum += all[i].Value
		if sum >= want {
			return all[:i+1], nil
		}
	}
	return nil, fmt.Errorf("insufficient funds: have %s, need %s PER", FormatAmount(sum), FormatAmount(want))
}

// DeploymentStatus computes the network's verdict on a proposed rule change
// from the active chain. The result depends only on the blocks themselves, so
// every node reaches the same answer without consulting anyone.
func (c *Chain) DeploymentStatus(d Deployment) (*DeploymentStatus, error) {
	st := &DeploymentStatus{Deployment: d, State: StateDefined}
	err := c.db.View(func(dbtx *bolt.Tx) error {
		tip := getU64(dbtx.Bucket(bMeta), kTipHeight)
		if tip < d.StartHeight {
			return nil
		}
		heights := dbtx.Bucket(bHeight)
		blocks := dbtx.Bucket(bBlocks)
		signalsIn := func(from, to uint64) (signals, counted uint64, err error) {
			for h := from; h < to; h++ {
				hash := heights.Get(be64(h))
				if hash == nil {
					return signals, counted, nil
				}
				raw := blocks.Get(hash)
				if raw == nil {
					return signals, counted, nil
				}
				hdr, err := DeserializeHeader(raw)
				if err != nil {
					return 0, 0, err
				}
				counted++
				if SignalsBit(hdr.Version, d.Bit) {
					signals++
				}
			}
			return signals, counted, nil
		}

		st.State = StateStarted
		for w := d.StartHeight; ; w += SignalWindow {
			end := w + SignalWindow
			if end > tip+1 {
				// Window still filling: report progress and stop.
				sig, cnt, err := signalsIn(w, tip+1)
				if err != nil {
					return err
				}
				st.WindowStart, st.WindowSignals, st.WindowBlocks = w, sig, cnt
				return nil
			}
			if w >= d.TimeoutHeight {
				st.State = StateFailed
				return nil
			}
			sig, _, err := signalsIn(w, end)
			if err != nil {
				return err
			}
			if sig >= SignalThreshold {
				st.State = StateLockedIn
				st.ActivationHeight = end + SignalWindow
				if tip >= st.ActivationHeight {
					st.State = StateActive
				}
				return nil
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return st, nil
}

// IsDeploymentActive reports whether a named rule change is in force at the
// current tip. Consensus code gates new rules on this and nothing else.
func (c *Chain) IsDeploymentActive(name string) (bool, error) {
	d, ok := DeploymentByName(name)
	if !ok {
		return false, nil
	}
	st, err := c.DeploymentStatus(d)
	if err != nil {
		return false, err
	}
	return st.State == StateActive, nil
}

type Stats struct {
	Height      uint64
	TipHash     [32]byte
	TipTime     int64
	Difficulty  *big.Int
	Emitted     uint64
	Burned      uint64
	Pool        uint64
	NextSubsidy uint64
	NextPayout  uint64
	Mempool     int
}

func (c *Chain) Stats() (*Stats, error) {
	s := &Stats{}
	err := c.db.View(func(dbtx *bolt.Tx) error {
		meta := dbtx.Bucket(bMeta)
		s.Height = getU64(meta, kTipHeight)
		copy(s.TipHash[:], meta.Get(kTip))
		hd, err := readHeaderAt(dbtx, s.Height)
		if err != nil {
			return err
		}
		s.TipTime = hd.Time
		s.Difficulty = new(big.Int).Div(MaxTarget, new(big.Int).SetBytes(hd.Target[:]))
		s.Emitted = getU64(meta, kEmitted)
		s.Burned = getU64(meta, kBurned)
		s.Pool = getU64(meta, kPool)
		s.NextSubsidy = BlockSubsidy(s.Height + 1)
		s.NextPayout = PoolPayout(s.Pool)
		s.Mempool = dbtx.Bucket(bMempool).Stats().KeyN
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}
