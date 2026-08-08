package core

import (
	"context"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type MineOpts struct {
	Logf    func(format string, a ...any)
	OnBlock func(*Block) // called after each locally mined block is accepted
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
	for count <= 0 || mined < count {
		if ctx.Err() != nil {
			return nil
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

// MinerThreads returns the mining parallelism: one worker per core, capped so
// the memory-hard PoW stays well under a gigabyte in total.
func MinerThreads() int {
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
