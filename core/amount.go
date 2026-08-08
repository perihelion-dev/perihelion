package core

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatAmount renders peri as a decimal PER string.
func FormatAmount(v uint64) string {
	s := fmt.Sprintf("%d.%08d", v/PER, v%PER)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// ParseAmount parses a decimal PER string ("1.5") into peri. Pure integer
// arithmetic — floats never touch monetary values.
func ParseAmount(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if whole == "" {
		whole = "0"
	}
	if len(frac) > 8 {
		return 0, fmt.Errorf("at most 8 decimal places")
	}
	frac += strings.Repeat("0", 8-len(frac))
	w, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	f, err := strconv.ParseUint(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if w > MaxSupply/PER {
		return 0, fmt.Errorf("amount exceeds maximum supply")
	}
	return w*PER + f, nil
}
