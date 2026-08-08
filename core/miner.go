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

// Mine mines count blocks paying rewards to addr (count <= 0: run until the
// context is cancelled). It parallelizes across CPU cores; each worker owns a
// disjoint nonce range, so a laptop and a solar-powered mini PC alike can mine.
func Mine(ctx context.Context, c *Chain, addr [32]byte, count int, logf func(format string, a ...any)) error {
	mined := 0
	for count <= 0 || mined < count {
		if ctx.Err() != nil {
			return nil
		}
		tmpl, err := c.NextBlockTemplate(addr)
		if err != nil {
			return err
		}
		solved, ok := solve(ctx, &tmpl.Header)
		if !ok {
			return nil // cancelled
		}
		tmpl.Header = *solved
		if err := c.AddBlock(tmpl); err != nil {
			return fmt.Errorf("mined block rejected: %w", err)
		}
		mined++
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

func solve(ctx context.Context, hdr *BlockHeader) (*BlockHeader, bool) {
	workers := MinerThreads()
	target := new(big.Int).SetBytes(hdr.Target[:])
	var found atomic.Bool
	result := make(chan BlockHeader, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			local := *hdr
			local.Nonce = uint64(w) << 56 // disjoint nonce space per worker
			for !found.Load() {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if local.Nonce&0xff == 0 {
					// keep the timestamp fresh during long solves
					if t := time.Now().Unix(); t > local.Time {
						local.Time = t
					}
				}
				ph := PowHash(local.Serialize(), local.PrevHash)
				if new(big.Int).SetBytes(ph[:]).Cmp(target) <= 0 {
					if found.CompareAndSwap(false, true) {
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
		return &h, true
	}
	return nil, false
}
