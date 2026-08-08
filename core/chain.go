package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bBlocks  = []byte("blocks")
	bHeight  = []byte("height")
	bUTXO    = []byte("utxo")
	bMeta    = []byte("meta")
	bMempool = []byte("mempool")

	kTip       = []byte("tip")
	kTipHeight = []byte("tipheight")
	kPool      = []byte("pool")
	kEmitted   = []byte("emitted")
	kBurned    = []byte("burned")
)

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

// Chain is the on-disk blockchain state: blocks, UTXO set, supply accounting
// and the local mempool. MVP scope: a strictly linear chain without reorgs;
// fork choice arrives with the P2P layer.
type Chain struct {
	db *bolt.DB
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
		for _, name := range [][]byte{bBlocks, bHeight, bUTXO, bMeta, bMempool} {
			if _, err := dbtx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		meta := dbtx.Bucket(bMeta)
		if meta.Get(kTip) != nil {
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
		if err := meta.Put(kTip, gh[:]); err != nil {
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

func (c *Chain) Close() error { return c.db.Close() }

// GenesisBlock is the hardcoded block 0. It carries no transactions and no
// reward — the fair-launch anchor of the timeline.
func GenesisBlock() *Block {
	return &Block{Header: BlockHeader{
		Version: 1,
		Height:  0,
		Time:    GenesisTime,
		Target:  TargetToBytes(InitialTarget()),
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

func readHeaderAt(dbtx *bolt.Tx, height uint64) (*BlockHeader, error) {
	hash := dbtx.Bucket(bHeight).Get(be64(height))
	if hash == nil {
		return nil, fmt.Errorf("no block at height %d", height)
	}
	raw := dbtx.Bucket(bBlocks).Get(hash)
	if raw == nil {
		return nil, fmt.Errorf("missing block body at height %d", height)
	}
	return DeserializeHeader(raw)
}

func recentHeaders(dbtx *bolt.Tx, tipHeight uint64, max int) ([]*BlockHeader, error) {
	start := uint64(0)
	if uint64(max) <= tipHeight {
		start = tipHeight - uint64(max) + 1
	}
	var out []*BlockHeader
	for h := start; h <= tipHeight; h++ {
		hd, err := readHeaderAt(dbtx, h)
		if err != nil {
			return nil, err
		}
		out = append(out, hd)
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
	for i := range t.Inputs {
		op := t.Inputs[i].Prev
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
		pendingSpent := map[OutPoint]bool{}
		err := mp.ForEach(func(k, v []byte) error {
			pt, err := DeserializeTx(v)
			if err != nil {
				return err
			}
			for i := range pt.Inputs {
				pendingSpent[pt.Inputs[i].Prev] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		next := getU64(dbtx.Bucket(bMeta), kTipHeight) + 1
		fee, err := checkTx(dbtx, t, next, pendingSpent)
		if err != nil {
			return err
		}
		if fee < MinRelayFee {
			return fmt.Errorf("fee %s below minimum %s PER", FormatAmount(fee), FormatAmount(MinRelayFee))
		}
		return mp.Put(id[:], raw)
	})
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
		hdrs, err := recentHeaders(dbtx, tipHeight, DifficultyWindow+1)
		if err != nil {
			return err
		}
		blk = &Block{
			Header: BlockHeader{
				Version:    1,
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

// AddBlock fully validates a solved block extending the tip and applies it to
// the UTXO set and supply accounting.
func (c *Chain) AddBlock(b *Block) error {
	raw := b.Serialize()
	if len(raw) > MaxBlockBytes {
		return fmt.Errorf("block too large")
	}
	return c.db.Update(func(dbtx *bolt.Tx) error {
		meta := dbtx.Bucket(bMeta)
		tipHeight := getU64(meta, kTipHeight)
		tip := meta.Get(kTip)
		hdr := &b.Header
		if hdr.Height != tipHeight+1 {
			return fmt.Errorf("height %d does not extend tip %d", hdr.Height, tipHeight)
		}
		if !bytes.Equal(hdr.PrevHash[:], tip) {
			return fmt.Errorf("prev hash does not match tip")
		}
		parent, err := readHeaderAt(dbtx, tipHeight)
		if err != nil {
			return err
		}
		if hdr.Time < parent.Time-TargetBlockTime {
			return fmt.Errorf("timestamp too far in the past")
		}
		if hdr.Time > time.Now().Unix()+MaxFutureDrift {
			return fmt.Errorf("timestamp too far in the future")
		}
		hdrs, err := recentHeaders(dbtx, tipHeight, DifficultyWindow+1)
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

		spent := map[OutPoint]bool{}
		var burnAdd, poolAdd uint64
		for i, t := range b.Txs[1:] {
			if t.IsCoinbase() {
				return fmt.Errorf("tx %d: unexpected coinbase", i+1)
			}
			fee, err := checkTx(dbtx, t, hdr.Height, spent)
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
		subsidy := BlockSubsidy(hdr.Height)
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

		// All checks passed — apply.
		utxos := dbtx.Bucket(bUTXO)
		for _, t := range b.Txs[1:] {
			for i := range t.Inputs {
				if err := utxos.Delete(utxoKey(t.Inputs[i].Prev)); err != nil {
					return err
				}
			}
		}
		for _, t := range b.Txs {
			id := t.ID()
			for i := range t.Outputs {
				u := &UTXO{
					Value:    t.Outputs[i].Value,
					Addr:     t.Outputs[i].Addr,
					Height:   hdr.Height,
					Coinbase: t.IsCoinbase(),
				}
				if err := utxos.Put(utxoKey(OutPoint{TxID: id, Index: uint32(i)}), u.serialize()); err != nil {
					return err
				}
			}
		}
		bh := hdr.Hash()
		if err := dbtx.Bucket(bBlocks).Put(bh[:], raw); err != nil {
			return err
		}
		if err := dbtx.Bucket(bHeight).Put(be64(hdr.Height), bh[:]); err != nil {
			return err
		}
		if err := meta.Put(kTip, bh[:]); err != nil {
			return err
		}
		if err := meta.Put(kTipHeight, be64(hdr.Height)); err != nil {
			return err
		}
		newEmitted := getU64(meta, kEmitted) + subsidy
		if newEmitted > MaxSupply {
			return fmt.Errorf("emission exceeds max supply — consensus bug")
		}
		if err := meta.Put(kEmitted, be64(newEmitted)); err != nil {
			return err
		}
		if err := meta.Put(kBurned, be64(getU64(meta, kBurned)+burnAdd)); err != nil {
			return err
		}
		if err := meta.Put(kPool, be64(poolBal+poolAdd-payout)); err != nil {
			return err
		}

		// Prune the mempool: drop anything whose inputs are no longer unspent
		// (this removes both included and now-conflicting transactions).
		mp := dbtx.Bucket(bMempool)
		var drop [][]byte
		err = mp.ForEach(func(k, v []byte) error {
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
			if err := mp.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
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
