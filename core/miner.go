package core

import (
	"context"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type MineOpts struct {
	Logf    func(format string, a ...any)
	OnBlock func(*Block) // called after each locally mined block is accepted

	// Ready gates mining. While it returns false the miner idles instead of
	// producing blocks — used to wait for initial synchronisation, so a new
	// node does not burn energy extending a branch that the network will
	// discard as soon as the real chain arrives.
	Ready func() bool
}

// Mine mines count blocks paying rewards to addr (count <= 0: run until the
// context is cancelled).
func Mine(ctx context.Context, c *Chain, addr [32]byte, count int, logf func(format string, a ...any)) error {
	return MineLoop(ctx, c, addr, count, MineOpts{Logf: logf})
}

// MineLoop is Mine with networking hooks: solving aborts the moment the chain
// tip changes (a peer's block arrived), so no work is wasted on stale blocks.
func MineLoop(ctx context.Context, c *Chain, addr [32]byte, count int, opts MineOpts) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	mined := 0
	waiting := false
	for count <= 0 || mined < count {
		if ctx.Err() != nil {
			return nil
		}
		if opts.Ready != nil && !opts.Ready() {
			if !waiting {
				logf("waiting for chain synchronisation before mining")
				waiting = true
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if waiting {
			logf("synchronised — mining")
			waiting = false
		}
		epoch := c.TipEpoch()
		tmpl, err := c.NextBlockTemplate(addr)
		if err != nil {
			return err
		}
		solved := solve(ctx, &tmpl.Header, func() bool { return c.TipEpoch() != epoch })
		if solved == nil {
			if ctx.Err() != nil {
				return nil
			}
			continue // tip moved while solving: rebuild template on the new tip
		}
		tmpl.Header = *solved
		if err := c.AcceptBlock(tmpl); err != nil {
			// A solved block being refused is normal when the tip moved under
			// us — the next iteration builds on the new tip. If the tip did
			// NOT move, the template itself is unacceptable (a clock far off,
			// a misconfigured network) and retrying forever would spin at
			// full CPU while looking like mining. Fail loudly instead.
			if c.TipEpoch() == epoch {
				return fmt.Errorf("own block rejected with tip unchanged — refusing to loop: %w", err)
			}
			logf("solved block no longer fits the chain (%v) — restarting on new tip", err)
			continue
		}
		mined++
		if opts.OnBlock != nil {
			opts.OnBlock(tmpl)
		}
		h := tmpl.Header.Hash()
		logf("block %d mined  hash=%x…  txs=%d  reward=%s PER",
			tmpl.Header.Height, h[:8], len(tmpl.Txs), FormatAmount(tmpl.Txs[0].Outputs[0].Value))
	}
	return nil
}

var minerThreadsOverride int

// SetMinerThreads overrides mining parallelism (0 restores automatic).
// Useful on shared machines: a seed VPS can mine gently with one thread.
func SetMinerThreads(n int) {
	if n < 0 {
		n = 0
	}
	if n > 8 {
		n = 8
	}
	minerThreadsOverride = n
}

// MinerThreads returns the mining parallelism: one worker per core, capped so
// the memory-hard PoW stays well under a gigabyte in total.
func MinerThreads() int {
	if minerThreadsOverride > 0 {
		return minerThreadsOverride
	}
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// solve returns the solved header, or nil if the context was cancelled or
// stale() reported that the chain tip moved.
func solve(ctx context.Context, hdr *BlockHeader, stale func() bool) *BlockHeader {
	workers := MinerThreads()
	target := new(big.Int).SetBytes(hdr.Target[:])
	var stop atomic.Bool
	result := make(chan BlockHeader, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := *hdr
			local.Nonce = uint64(w) << 56 // disjoint nonce space per worker
			for !stop.Load() {
				select {
				case <-ctx.Done():
					stop.Store(true)
					return
				default:
				}
				if local.Nonce&0xff == 0 {
					if stale != nil && stale() {
						stop.Store(true)
						return
					}
					// keep the timestamp fresh during long solves
					if t := time.Now().Unix(); t > local.Time {
						local.Time = t
					}
				}
				ph := PowHash(local.Serialize(), local.PrevHash)
				if new(big.Int).SetBytes(ph[:]).Cmp(target) <= 0 {
					if !stop.Swap(true) {
						result <- local
					}
					return
				}
				local.Nonce++
			}
		}(w)
	}
	wg.Wait()
	close(result)
	if h, ok := <-result; ok {
		return &h
	}
	return nil
}
