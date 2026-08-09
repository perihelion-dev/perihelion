package core

// Consensus rule changes in Perihelion are activated by the network, not by
// any maintainer, foundation or release. A proposed change is assigned a
// signal bit; miners set that bit in the blocks they produce if they are
// willing to adopt it. Every node independently counts the signals in the
// chain it has verified and reaches the same conclusion. Nobody can activate
// a rule by publishing software, and nobody can veto one that the network has
// adopted — activation is a property of the blockchain itself.
//
// Two things sit permanently outside this mechanism:
//
//   - Monetary policy. The emission schedule, the supply bound and the fee
//     burn cannot be changed by any deployment. A promise that a majority can
//     vote away is not a promise, and a currency whose supply is negotiable
//     has no anchor. There is no signal bit that touches these rules.
//   - The right to refuse. Signalling decides when a prepared change takes
//     effect; it never obliges anyone to run it. A node that declines an
//     activated rule simply follows the chain it considers valid.

// Signal bits live in the low 29 bits of the block version; the top three
// bits carry a fixed marker so that ordinary blocks are distinguishable from
// future version schemes. Version 1 (no signalling) remains valid forever.
const (
	VersionSignalBase uint32 = 0x2000_0000
	VersionTopMask    uint32 = 0xE000_0000
	VersionBitsMask   uint32 = 0x1FFF_FFFF
	MaxSignalBit      uint32 = 28

	// SignalWindow is the tally period: 10,080 blocks ≈ 7 days at 60s.
	SignalWindow uint64 = 10_080

	// SignalThreshold is the number of signalling blocks required within one
	// window for a deployment to lock in (90%).
	SignalThreshold uint64 = 9_072
)

// ValidBlockVersion reports whether a block version is well-formed: either the
// original unsignalled version 1, or the signalling form with the marker set.
func ValidBlockVersion(v uint32) bool {
	return v == 1 || v&VersionTopMask == VersionSignalBase
}

// SignalsBit reports whether a block version signals support for bit b.
func SignalsBit(v uint32, b uint32) bool {
	if b > MaxSignalBit || v&VersionTopMask != VersionSignalBase {
		return false
	}
	return v&(1<<b) != 0
}

// Deployment is a proposed consensus rule change awaiting network approval.
//
//   - StartHeight: the first height at which signals are counted.
//   - TimeoutHeight: if the threshold is not met in any completed window that
//     begins before this height, the proposal fails permanently.
//
// A locked-in deployment becomes active one full window later, so that
// everyone has roughly a week of notice after the outcome is decided.
type Deployment struct {
	Name          string
	Bit           uint32
	StartHeight   uint64
	TimeoutHeight uint64
}

// Deployments is the list of rule changes the network may vote on. It is
// empty: Perihelion launched with the rules it intends to keep. Entries are
// added only when a change has been proposed, reviewed in the open and
// implemented — and even then the network decides whether it ever takes
// effect.
var Deployments = []Deployment{}

// DeploymentByName returns a proposed deployment by name.
func DeploymentByName(name string) (Deployment, bool) {
	for _, d := range Deployments {
		if d.Name == name {
			return d, true
		}
	}
	return Deployment{}, false
}

type ThresholdState int

const (
	StateDefined  ThresholdState = iota // signalling has not started
	StateStarted                        // signals are being counted
	StateLockedIn                       // threshold met; activation pending
	StateActive                         // rule is in force
	StateFailed                         // timed out without reaching threshold
)

func (s ThresholdState) String() string {
	switch s {
	case StateDefined:
		return "defined"
	case StateStarted:
		return "started"
	case StateLockedIn:
		return "locked_in"
	case StateActive:
		return "active"
	case StateFailed:
		return "failed"
	}
	return "unknown"
}

// windowStart returns the first height of the tally window containing h,
// aligned to the deployment's start.
func windowStart(d Deployment, h uint64) uint64 {
	if h < d.StartHeight {
		return d.StartHeight
	}
	return d.StartHeight + ((h-d.StartHeight)/SignalWindow)*SignalWindow
}

// minerSignalBits is what this node's miner signals support for. It is a local
// choice: every operator decides independently, which is precisely what makes
// the tally a measurement of the network rather than of any one participant.
var minerSignalBits uint32

// SetMinerSignalBits sets the deployment bits this node's miner will signal.
func SetMinerSignalBits(bits uint32) { minerSignalBits = bits & VersionBitsMask }

// MinerSignalBits returns the bits this node currently signals.
func MinerSignalBits() uint32 { return minerSignalBits }

// DeploymentStatus is the network's verdict on a proposal, as computed from
// the chain. Two nodes with the same chain always compute the same status.
type DeploymentStatus struct {
	Deployment       Deployment
	State            ThresholdState
	ActivationHeight uint64 // set when locked in or active
	WindowStart      uint64 // current tally window (when counting)
	WindowSignals    uint64 // signalling blocks so far in that window
	WindowBlocks     uint64 // blocks counted so far in that window
}
