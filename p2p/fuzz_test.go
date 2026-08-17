package p2p

import "testing"

// Fuzz targets for the wire protocol decoders. These sit directly behind the
// network socket, so a panic here is a remote crash of every node. See the
// core fuzz targets for the philosophy; the property is the same: never
// panic, and anything that decodes must re-encode to something that decodes
// identically.

func FuzzDecodeHello(f *testing.F) {
	f.Add(encodeHello(1, 100, [32]byte{9}, "1.2.3.4:16180"))
	f.Add(encodeHello(1, 0, [32]byte{}, ""))
	f.Add([]byte{})
	f.Add(make([]byte, 45)) // one byte past the fixed part, malformed length
	f.Fuzz(func(t *testing.T, data []byte) {
		ver, height, tip, addr, err := decodeHello(data)
		if err != nil {
			return
		}
		re := encodeHello(ver, height, tip, addr)
		v2, h2, t2, a2, err := decodeHello(re)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if v2 != ver || h2 != height || t2 != tip || a2 != addr {
			t.Fatal("hello round trip changed a field")
		}
	})
}

func FuzzDecodeAddrs(f *testing.F) {
	f.Add(encodeAddrs([]string{"1.2.3.4:16180", "[::1]:16180"}))
	f.Add(encodeAddrs(nil))
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff}) // claims 65535 entries
	f.Fuzz(func(t *testing.T, data []byte) {
		addrs, err := decodeAddrs(data)
		if err != nil {
			return
		}
		if len(addrs) > MaxAddrPerMessage {
			t.Fatalf("decoded %d addresses, above the cap of %d", len(addrs), MaxAddrPerMessage)
		}
		for _, a := range addrs {
			if len(a) > MaxAddrLen {
				t.Fatalf("decoded an address of %d bytes, above the cap of %d", len(a), MaxAddrLen)
			}
		}
		re := encodeAddrs(addrs)
		back, err := decodeAddrs(re)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if len(back) != len(addrs) {
			t.Fatal("addr round trip changed the count")
		}
	})
}

func FuzzDecodeGetBlocks(f *testing.F) {
	f.Add(encodeGetBlocks(1, 200))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		from, count, err := decodeGetBlocks(data)
		if err != nil {
			return
		}
		f2, c2, err := decodeGetBlocks(encodeGetBlocks(from, count))
		if err != nil || f2 != from || c2 != count {
			t.Fatal("getblocks round trip failed")
		}
	})
}
