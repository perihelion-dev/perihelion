package p2p

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// addrBook remembers endpoints of nodes that accept inbound connections, so
// that a node stops depending on its configured seeds once it has been online.
//
// The threat this must survive is the eclipse attack: an adversary feeds a
// victim so many endpoints under its control that every connection the victim
// makes leads back to the adversary, who can then hide the real chain from it.
// Three limits make that expensive: the book is capped, each peer may
// contribute only a bounded share of it, and outbound connections are spread
// across distinct network prefixes so that owning one hosting range is not
// enough.
type addrBook struct {
	mu    sync.Mutex
	seen  map[string]addrRecord
	path  string
	dirty bool
}

type addrRecord struct {
	LastSeen time.Time
	Source   string // peer we learned it from; "" for operator-configured
}

const (
	maxAddrBook       = 512
	maxPerSource      = 32 // one peer cannot dominate the book
	addrExpiry        = 14 * 24 * time.Hour
	targetOutbound    = 8
	maxPerGroup       = 2 // outbound connections sharing a /16 (IPv4) or /32 (IPv6)
	discoveryInterval = 30 * time.Second
)

func newAddrBook() *addrBook {
	return &addrBook{seen: map[string]addrRecord{}}
}

// normalizeAddr validates an endpoint and returns it in canonical form.
func normalizeAddr(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxAddrLen {
		return "", fmt.Errorf("invalid address")
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return "", err
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("invalid address")
	}
	if p := net.ParseIP(host); p != nil {
		return net.JoinHostPort(p.String(), port), nil
	}
	// Hostnames are allowed so that seeds can be DNS names.
	if strings.ContainsAny(host, " \t/\\") {
		return "", fmt.Errorf("invalid host")
	}
	return net.JoinHostPort(host, port), nil
}

// routable reports whether an address is one we should dial. Loopback and
// private ranges are accepted only when the peer that told us about them is
// itself local, which keeps LAN and test setups working without letting a
// remote peer point us at addresses inside our own network.
func routable(addr string, sourceIsLocal bool) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true // hostname: resolved at dial time
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return sourceIsLocal
	}
	return true
}

// group returns the coarse network an address belongs to, used to spread
// outbound connections so that one operator's address range cannot supply all
// of a node's peers.
func group(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("v4:%d.%d", v4[0], v4[1])
	}
	return "v6:" + ip.Mask(net.CIDRMask(32, 128)).String()
}

// add records an endpoint. source is the peer that supplied it.
func (b *addrBook) add(addr, source string) bool {
	norm, err := normalizeAddr(addr)
	if err != nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.seen[norm]; exists {
		rec := b.seen[norm]
		rec.LastSeen = time.Now()
		b.seen[norm] = rec
		return false
	}
	if source != "" {
		n := 0
		for _, rec := range b.seen {
			if rec.Source == source {
				n++
			}
		}
		if n >= maxPerSource {
			return false
		}
	}
	if len(b.seen) >= maxAddrBook {
		b.evictOldestLocked()
	}
	b.seen[norm] = addrRecord{LastSeen: time.Now(), Source: source}
	b.dirty = true
	return true
}

func (b *addrBook) evictOldestLocked() {
	var oldest string
	var oldestT time.Time
	for a, rec := range b.seen {
		if rec.Source == "" {
			continue // never evict operator-configured seeds
		}
		if oldest == "" || rec.LastSeen.Before(oldestT) {
			oldest, oldestT = a, rec.LastSeen
		}
	}
	if oldest != "" {
		delete(b.seen, oldest)
	}
}

// sample returns known addresses to share with a peer, newest first.
func (b *addrBook) sample(limit int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	type kv struct {
		addr string
		t    time.Time
	}
	all := make([]kv, 0, len(b.seen))
	for a, rec := range b.seen {
		if time.Since(rec.LastSeen) < addrExpiry {
			all = append(all, kv{a, rec.LastSeen})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.addr
	}
	return out
}

// candidates returns addresses worth dialling: not already connected, and not
// in a network group that already supplies enough of our outbound peers.
func (b *addrBook) candidates(connected map[string]bool, groups map[string]int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for a, rec := range b.seen {
		if connected[a] || time.Since(rec.LastSeen) > addrExpiry {
			continue
		}
		if groups[group(a)] >= maxPerGroup {
			continue
		}
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// RoutableForTest exposes the dialability rule to tests in other packages.
func RoutableForTest(addr string, sourceIsLocal bool) bool {
	norm, err := normalizeAddr(addr)
	if err != nil {
		return false
	}
	return routable(norm, sourceIsLocal)
}

func (b *addrBook) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.seen)
}

// load reads a previously saved book and enables persistence at path.
func (b *addrBook) load(path string) {
	b.mu.Lock()
	b.path = path
	b.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			b.add(line, "disk")
		}
	}
}

func (b *addrBook) save() {
	b.mu.Lock()
	path, dirty := b.path, b.dirty
	if path == "" || !dirty {
		b.mu.Unlock()
		return
	}
	lines := make([]string, 0, len(b.seen))
	for a, rec := range b.seen {
		if time.Since(rec.LastSeen) < addrExpiry {
			lines = append(lines, a)
		}
	}
	b.dirty = false
	b.mu.Unlock()

	sort.Strings(lines)
	body := "# Perihelion known peers — maintained automatically\n" + strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(body), 0o600) == nil {
		os.Rename(tmp, path)
	}
}
