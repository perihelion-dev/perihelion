package core

// HeaderSize is the fixed serialized header length in bytes.
const HeaderSize = 4 + 8 + 32 + 32 + 8 + 32 + 8

type BlockHeader struct {
	Version    uint32
	Height     uint64
	PrevHash   [32]byte
	MerkleRoot [32]byte
	Time       int64
	Target     [32]byte // big-endian PoW target; the Argon2id hash must be <= Target
	Nonce      uint64
}

func (h *BlockHeader) Serialize() []byte {
	w := &buf{}
	w.u32(h.Version)
	w.u64(h.Height)
	w.raw(h.PrevHash[:])
	w.raw(h.MerkleRoot[:])
	w.i64(h.Time)
	w.raw(h.Target[:])
	w.u64(h.Nonce)
	return w.b
}

func (h *BlockHeader) Hash() [32]byte { return H([]byte("PER:block"), h.Serialize()) }

func DeserializeHeader(b []byte) (*BlockHeader, error) {
	if len(b) < HeaderSize {
		return nil, errTruncated
	}
	r := &rdr{b: b[:HeaderSize]}
	h := &BlockHeader{}
	h.Version = r.u32()
	h.Height = r.u64()
	h.PrevHash = r.arr32()
	h.MerkleRoot = r.arr32()
	h.Time = r.i64()
	h.Target = r.arr32()
	h.Nonce = r.u64()
	if err := r.done(); err != nil {
		return nil, err
	}
	return h, nil
}

type Block struct {
	Header BlockHeader
	Txs    []*Tx
}

func (b *Block) Serialize() []byte {
	w := &buf{}
	w.raw(b.Header.Serialize())
	w.u32(uint32(len(b.Txs)))
	for _, t := range b.Txs {
		w.bytes(t.Serialize())
	}
	return w.b
}

func DeserializeBlock(raw []byte) (*Block, error) {
	if len(raw) < HeaderSize+4 {
		return nil, errTruncated
	}
	hdr, err := DeserializeHeader(raw)
	if err != nil {
		return nil, err
	}
	r := &rdr{b: raw, off: HeaderSize}
	n := int(r.u32())
	if n > 100_000 {
		return nil, errTruncated
	}
	blk := &Block{Header: *hdr}
	for i := 0; i < n; i++ {
		tb := r.bytes(MaxBlockBytes)
		if tb == nil {
			return nil, errTruncated
		}
		t, err := DeserializeTx(tb)
		if err != nil {
			return nil, err
		}
		blk.Txs = append(blk.Txs, t)
	}
	if err := r.done(); err != nil {
		return nil, err
	}
	return blk, nil
}

// MerkleRoot commits to the ordered list of transaction IDs (SHA3-256 tree).
func MerkleRoot(txs []*Tx) [32]byte {
	if len(txs) == 0 {
		return [32]byte{}
	}
	layer := make([][32]byte, len(txs))
	for i, t := range txs {
		layer[i] = t.ID()
	}
	for len(layer) > 1 {
		if len(layer)%2 == 1 {
			layer = append(layer, layer[len(layer)-1])
		}
		next := make([][32]byte, 0, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next = append(next, H([]byte("PER:merkle"), layer[i][:], layer[i+1][:]))
		}
		layer = next
	}
	return layer[0]
}
