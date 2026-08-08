package core

import (
	"crypto/sha3"
	"encoding/binary"
	"errors"
)

// H computes SHA3-256 over the concatenation of parts. Callers pass a leading
// domain-separation tag so hashes from different contexts can never collide.
func H(parts ...[]byte) [32]byte {
	h := sha3.New256()
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// buf is a little-endian serializer.
type buf struct{ b []byte }

func (w *buf) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	w.b = append(w.b, t[:]...)
}

func (w *buf) u64(v uint64) {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], v)
	w.b = append(w.b, t[:]...)
}

func (w *buf) i64(v int64) { w.u64(uint64(v)) }

func (w *buf) raw(p []byte) { w.b = append(w.b, p...) }

func (w *buf) bytes(p []byte) {
	w.u32(uint32(len(p)))
	w.raw(p)
}

var errTruncated = errors.New("truncated or malformed data")

// rdr is the matching deserializer. All returned byte slices are copies, so
// they stay valid after the underlying (possibly memory-mapped) buffer is gone.
type rdr struct {
	b   []byte
	off int
	err error
}

func (r *rdr) take(n int) []byte {
	if r.err != nil || n < 0 || r.off+n > len(r.b) {
		if r.err == nil {
			r.err = errTruncated
		}
		return nil
	}
	out := make([]byte, n)
	copy(out, r.b[r.off:r.off+n])
	r.off += n
	return out
}

func (r *rdr) u32() uint32 {
	p := r.take(4)
	if p == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(p)
}

func (r *rdr) u64() uint64 {
	p := r.take(8)
	if p == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(p)
}

func (r *rdr) i64() int64 { return int64(r.u64()) }

func (r *rdr) bytes(max int) []byte {
	n := int(r.u32())
	if n > max {
		if r.err == nil {
			r.err = errTruncated
		}
		return nil
	}
	return r.take(n)
}

func (r *rdr) arr32() [32]byte {
	var out [32]byte
	p := r.take(32)
	if p != nil {
		copy(out[:], p)
	}
	return out
}

func (r *rdr) done() error {
	if r.err != nil {
		return r.err
	}
	if r.off != len(r.b) {
		return errTruncated
	}
	return nil
}
