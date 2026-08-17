package core

import (
	"fmt"
	"math/big"
	"sync/atomic"
)

// Network bundles every value that must differ between mainnet, testnet and
// regtest. Everything else — the emission formula, the signature scheme, the
// block structure, the validation rules — is identical across networks, which
// is the point: a testnet is only useful if what it tests is the real thing.
//
// The separation is total on every layer that could cause confusion:
//
//   - Genesis differs, so a testnet coin can never be a mainnet coin — the two
//     chains share no history and no output.
//   - Address prefix differs (per1 / tper1 / rper1), so a testnet address is
//     rejected outright by a mainnet wallet and vice versa. Money cannot be
//     sent to the wrong network by mistake.
//   - Chain-ID differs, so a signature made on one network is meaningless on
//     another even if someone contrived a shared output.
//   - Wire magic and port differ, so nodes of different networks refuse to
//     even complete a handshake, and cannot accidentally sync each other's
//     chains.
//   - Data directory differs, so a machine can run several networks side by
//     side without one wallet or chain overwriting another.
type Network struct {
	Name           string
	AddressPrefix  string // "per1", "tper1", "rper1"
	GenesisTime    int64
	GenesisMessage string
	MinDifficulty  *big.Int
	WireMagic      uint32
	DefaultPort    int
	DataDirSuffix  string   // "" for mainnet, "-testnet", "-regtest"
	Seeds          []string // bootstrap peers; empty for regtest
	Checkpoints    map[uint64][32]byte
	// AllowMinDifficultyBlocks: on regtest, blocks may always be produced at
	// minimum difficulty regardless of timing, so tests never wait.
	AllowMinDifficultyBlocks bool
}

var (
	// Mainnet is the real Perihelion network. Its parameters are what every
	// node has enforced since 2026-08-09 and must never change here.
	Mainnet = &Network{
		Name:           "mainnet",
		AddressPrefix:  "per1",
		GenesisTime:    1_786_233_600, // 2026-08-09 00:00:00 UTC
		GenesisMessage: "Perihelion genesis 2026-08-09 — quantum-safe money, mined by everyone, owned by no one",
		MinDifficulty:  big.NewInt(16),
		WireMagic:      0x50455231, // "PER1"
		DefaultPort:    16180,
		DataDirSuffix:  "",
		Seeds: []string{
			"186.240.157.169:16180",
			"187.124.167.107:16180",
		},
		Checkpoints: map[uint64][32]byte{
			12000: mustHash("138825d4f146c8aa85281eaf517664f2704860707dabd43ce449d373e66d0570"),
		},
	}

	// Testnet is a public network for trying things out. Coins there are worth
	// nothing by construction and can never be confused with mainnet PER: the
	// address prefix alone makes a testnet address invalid on mainnet.
	// Difficulty floor is low so a single machine can keep it alive.
	Testnet = &Network{
		Name:           "testnet",
		AddressPrefix:  "tper1",
		GenesisTime:    1_786_924_800, // 2026-08-17 00:00:00 UTC
		GenesisMessage: "Perihelion TESTNET genesis 2026-08-17 — coins here are worthless by design",
		MinDifficulty:  big.NewInt(4),
		WireMagic:      0x50455254, // "PERT"
		DefaultPort:    26180,
		DataDirSuffix:  "-testnet",
		Seeds:          []string{"186.240.157.169:26180"},
		Checkpoints:    map[uint64][32]byte{},
	}

	// Regtest is a private network for a single machine or a test suite: no
	// seeds, trivial difficulty, and blocks may always be mined at the floor.
	// Nothing on regtest is ever shared with anyone.
	Regtest = &Network{
		Name:                     "regtest",
		AddressPrefix:            "rper1",
		GenesisTime:              1_700_000_000, // 2023-11-14 — fixed, always in the past
		GenesisMessage:           "Perihelion REGTEST genesis — private, disposable, never shared",
		MinDifficulty:            big.NewInt(1),
		WireMagic:                0x50455252, // "PERR"
		DefaultPort:              36180,
		DataDirSuffix:            "-regtest",
		Seeds:                    nil,
		Checkpoints:              map[uint64][32]byte{},
		AllowMinDifficultyBlocks: true,
	}
)

// active is the network this process runs on. Selected once at startup and
// never changed while running: mixing networks in one process would defeat
// every separation above.
var active atomic.Pointer[Network]

func init() { active.Store(Mainnet) }

// Active returns the network this process is configured for.
func Active() *Network { return active.Load() }

// SelectNetwork chooses the network for this process. Call it before opening
// a chain, creating a wallet or starting a node.
func SelectNetwork(n *Network) {
	if n == nil {
		panic("SelectNetwork(nil)")
	}
	active.Store(n)
	// Mirror into the package-level variables that older code and tests read
	// directly, so every consensus path agrees on the same values.
	MinDifficulty = new(big.Int).Set(n.MinDifficulty)
	Checkpoints = n.Checkpoints
}

// NetworkByName maps a flag value to a network.
func NetworkByName(name string) (*Network, error) {
	switch name {
	case "", "mainnet", "main":
		return Mainnet, nil
	case "testnet", "test":
		return Testnet, nil
	case "regtest", "reg":
		return Regtest, nil
	}
	return nil, fmt.Errorf("unknown network %q (use mainnet, testnet or regtest)", name)
}
