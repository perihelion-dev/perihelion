package core

// Checkpoints are block hashes at fixed heights that a node treats as known
// good. They exist for one reason: to make joining fast.
//
// Without them a fresh node must run the full Argon2id proof-of-work check —
// 64 MiB of memory work — on every block since genesis before it can do
// anything, and that cost grows forever. With them, a node still downloads
// and structurally verifies every block, still checks every transaction and
// signature, still recomputes every difficulty target and every coinbase; the
// only thing it skips is re-doing the memory-hard hash on blocks that are
// already buried thousands of blocks beneath a hash the software ships with.
//
// What this does NOT do:
//
//   - It does not change consensus. A checkpoint is not a rule; a chain that
//     disagrees with a checkpoint is one this software will not follow, but
//     that is a property of this software, exactly as the genesis block is.
//   - It does not let anyone rewrite history. A checkpoint can only ever be
//     added at a height already deep in the past, where the network long ago
//     agreed; it pins what already happened, it cannot make anything happen.
//   - It does not trust anyone. Anyone can confirm each hash below against
//     their own fully-validated node or against the public explorer, and any
//     software that ships a wrong one simply forks itself off the network.
//
// Every checkpoint carries the date it was added, so its age is visible.
var Checkpoints = Mainnet.Checkpoints

// LastCheckpointHeight is the highest checkpointed height, or 0.
func LastCheckpointHeight() uint64 {
	var best uint64
	for h := range Checkpoints {
		if h > best {
			best = h
		}
	}
	return best
}

// checkpointConflict reports whether a block at a checkpointed height carries
// a different hash than the one this software knows to be good.
func checkpointConflict(height uint64, hash [32]byte) bool {
	want, ok := Checkpoints[height]
	return ok && want != hash
}

// belowLastCheckpoint reports whether proof-of-work may be assumed for a
// block: true when it sits at or below the newest checkpoint AND the chain
// being extended actually passes through that checkpoint. The second
// condition matters — a block "below the checkpoint height" on a branch that
// never reaches the checkpoint hash is not covered by it and must be checked
// in full.
func belowLastCheckpoint(height uint64) bool {
	return height <= LastCheckpointHeight()
}

func mustHash(hexStr string) [32]byte {
	var h [32]byte
	if len(hexStr) != 64 {
		panic("checkpoint hash must be 64 hex characters")
	}
	for i := 0; i < 32; i++ {
		var b byte
		for j := 0; j < 2; j++ {
			c := hexStr[i*2+j]
			var v byte
			switch {
			case c >= '0' && c <= '9':
				v = c - '0'
			case c >= 'a' && c <= 'f':
				v = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v = c - 'A' + 10
			default:
				panic("checkpoint hash contains non-hex character")
			}
			b = b<<4 | v
		}
		h[i] = b
	}
	return h
}
