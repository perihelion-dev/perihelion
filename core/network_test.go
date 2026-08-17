package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"perihelion/core"
	"perihelion/wallet"
)

// withNetwork runs f with the given network selected, restoring mainnet
// afterwards so other tests are unaffected.
func withNetwork(t *testing.T, n *core.Network, f func()) {
	t.Helper()
	core.SelectNetwork(n)
	defer core.SelectNetwork(core.Mainnet)
	f()
}

// TestNetworksAreSeparate: the whole point of a testnet is that nothing on
// it can be confused with mainnet. Every layer that could leak must differ.
func TestNetworksAreSeparate(t *testing.T) {
	nets := []*core.Network{core.Mainnet, core.Testnet, core.Regtest}

	// Genesis, chain-ID, prefix, magic and port all pairwise distinct.
	for i, a := range nets {
		for j, b := range nets {
			if i >= j {
				continue
			}
			var ga, gb [32]byte
			var ida, idb [32]byte
			withNetwork(t, a, func() { ga = core.GenesisBlock().Header.Hash(); ida = core.ChainID() })
			withNetwork(t, b, func() { gb = core.GenesisBlock().Header.Hash(); idb = core.ChainID() })
			if ga == gb {
				t.Errorf("%s and %s share a genesis hash", a.Name, b.Name)
			}
			if ida == idb {
				t.Errorf("%s and %s share a chain-ID", a.Name, b.Name)
			}
			if a.AddressPrefix == b.AddressPrefix {
				t.Errorf("%s and %s share an address prefix", a.Name, b.Name)
			}
			if a.WireMagic == b.WireMagic {
				t.Errorf("%s and %s share wire magic", a.Name, b.Name)
			}
			if a.DefaultPort == b.DefaultPort {
				t.Errorf("%s and %s share a port", a.Name, b.Name)
			}
			if a.DataDirSuffix == b.DataDirSuffix {
				t.Errorf("%s and %s share a data directory", a.Name, b.Name)
			}
		}
	}
}

// TestAddressesDoNotCrossNetworks: a testnet address must be rejected by a
// mainnet node with an error that says why — sending real coins to a
// testnet address by mistake is exactly the accident the prefix prevents.
func TestAddressesDoNotCrossNetworks(t *testing.T) {
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	var mainAddr, testAddr string
	withNetwork(t, core.Mainnet, func() { mainAddr = core.EncodeAddress(w.Address()) })
	withNetwork(t, core.Testnet, func() { testAddr = core.EncodeAddress(w.Address()) })

	if !strings.HasPrefix(mainAddr, "per1") || !strings.HasPrefix(testAddr, "tper1") {
		t.Fatalf("prefixes wrong: %s / %s", mainAddr[:6], testAddr[:6])
	}
	// Same key, different encodings — and each is refused on the other net.
	withNetwork(t, core.Mainnet, func() {
		if _, err := core.DecodeAddress(testAddr); err == nil {
			t.Fatal("mainnet accepted a testnet address")
		} else if !strings.Contains(err.Error(), "testnet") {
			t.Fatalf("mainnet should name the foreign network in its error, got: %v", err)
		}
		if _, err := core.DecodeAddress(mainAddr); err != nil {
			t.Fatalf("mainnet rejected its own address: %v", err)
		}
	})
	withNetwork(t, core.Testnet, func() {
		if _, err := core.DecodeAddress(mainAddr); err == nil {
			t.Fatal("testnet accepted a mainnet address")
		}
	})
}

// TestSignaturesDoNotCrossNetworks: a chain-bound signature made on testnet
// must not verify on mainnet, even for an identical transaction and key.
func TestSignaturesDoNotCrossNetworks(t *testing.T) {
	w, err := wallet.New()
	if err != nil {
		t.Fatal(err)
	}
	tx := &core.Tx{
		Inputs:  []core.TxInput{{Prev: core.OutPoint{TxID: [32]byte{9}, Index: 0}, PubKey: w.PubBytes()}},
		Outputs: []core.TxOutput{{Value: 1, Addr: w.Address()}},
	}
	addrOf := func(core.OutPoint) ([32]byte, bool) { return core.AddrOfPubKey(w.PubBytes()), true }
	// Sign on testnet with the chain-bound digest.
	withNetwork(t, core.Testnet, func() {
		d := tx.SigDigestV2()
		tx.Inputs[0].Signature = w.Sign(d[:])
	})
	// Verify strictly (past the switchover) on mainnet: must fail.
	withNetwork(t, core.Mainnet, func() {
		if err := tx.VerifySignatures(addrOf, core.SighashChainIDHeight); err == nil {
			t.Fatal("a testnet signature verified on mainnet — chain-ID does not separate them")
		}
	})
}

// TestDatabaseRefusesWrongNetwork: a data directory created on one network
// must not open under another; the stored chain would be nonsense there and
// the wallet beside it would hold coins that do not exist on that chain.
func TestDatabaseRefusesWrongNetwork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.db")
	withNetwork(t, core.Testnet, func() {
		c, err := core.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	})
	withNetwork(t, core.Mainnet, func() {
		if _, err := core.Open(path); err == nil {
			t.Fatal("a testnet database opened as mainnet")
		} else if !strings.Contains(err.Error(), "testnet") {
			t.Fatalf("error should name the database's network, got: %v", err)
		}
	})
}

// TestRegtestMinesInstantly: regtest exists so tests and local experiments
// never wait. A block must be found in well under a second.
func TestRegtestMinesInstantly(t *testing.T) {
	withNetwork(t, core.Regtest, func() {
		oldMem := core.PowMemoryKiB
		core.PowMemoryKiB = 8
		defer func() { core.PowMemoryKiB = oldMem }()
		c := openTestChain(t)
		w, _ := wallet.New()
		if err := core.Mine(context.Background(), c, w.Address(), 5, nil); err != nil {
			t.Fatal(err)
		}
		if h, _, _ := c.TipInfo(); h != 5 {
			t.Fatalf("regtest height %d, want 5", h)
		}
		if !strings.HasPrefix(core.EncodeAddress(w.Address()), "rper1") {
			t.Fatal("regtest addresses should start with rper1")
		}
	})
}
