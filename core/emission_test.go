package core_test

import (
	"testing"

	"perihelion/core"
)

func TestSubsidySchedule(t *testing.T) {
	if core.BlockSubsidy(0) != 0 {
		t.Fatal("genesis must carry no subsidy (fair launch)")
	}
	if core.BlockSubsidy(1) != core.InitialReward {
		t.Fatalf("subsidy(1) = %d, want %d", core.BlockSubsidy(1), core.InitialReward)
	}
	prev := core.BlockSubsidy(1)
	for _, h := range []uint64{2, 10, 1_000, 100_000, 2_079_442, 10_000_000, 62_000_000} {
		s := core.BlockSubsidy(h)
		if s > prev {
			t.Fatalf("subsidy must never increase: subsidy(%d) = %d > %d", h, s, prev)
		}
		prev = s
	}
	// Half-life: ~2,079,441 blocks (≈ 3.95 years). Allow 0.1% tolerance.
	half := core.BlockSubsidy(2_079_442)
	ratio := float64(half) / float64(core.InitialReward)
	if ratio < 0.4995 || ratio > 0.5005 {
		t.Fatalf("half-life off: ratio = %f", ratio)
	}
	// Emission fades to zero in the distant future (~year 2159); after that
	// miners live entirely from the recycled fee pool.
	if core.BlockSubsidy(70_000_000) != 0 {
		t.Fatal("subsidy should reach zero eventually")
	}
}

func TestFeeSplit(t *testing.T) {
	burn, pool := core.SplitFee(100_000)
	if burn != 50_000 || pool != 50_000 {
		t.Fatalf("split(100000) = %d/%d", burn, pool)
	}
	burn, pool = core.SplitFee(101)
	if burn != 50 || pool != 51 || burn+pool != 101 {
		t.Fatalf("split(101) = %d/%d", burn, pool)
	}
}

func TestAmountRoundtrip(t *testing.T) {
	cases := map[string]uint64{
		"1.5":        150_000_000,
		"0.00000001": 1,
		"10":         1_000_000_000,
		"0.001":      100_000,
	}
	for s, want := range cases {
		got, err := core.ParseAmount(s)
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", s, err)
		}
		if got != want {
			t.Fatalf("ParseAmount(%q) = %d, want %d", s, got, want)
		}
	}
	if core.FormatAmount(150_000_000) != "1.5" {
		t.Fatalf("FormatAmount(150000000) = %q", core.FormatAmount(150_000_000))
	}
	if _, err := core.ParseAmount("1.123456789"); err == nil {
		t.Fatal("expected error for >8 decimals")
	}
	if _, err := core.ParseAmount("abc"); err == nil {
		t.Fatal("expected error for garbage")
	}
}
