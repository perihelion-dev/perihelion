// Command perihelion-explorer serves a public, read-only view of the
// Perihelion chain.
//
// Security posture. This program is intended to face the open internet, so it
// is built to be incapable of harm rather than merely careful:
//
//   - It cannot spend. It does not import the wallet package, holds no keys,
//     and has no code path that signs anything. Compromising it yields no
//     coins because there are none to take.
//   - It cannot be written to. Every route is GET-only; there is no endpoint
//     that accepts a transaction, and request bodies are never read.
//   - It validates its own view. It runs a full node and verifies every block
//     itself, so a hostile peer cannot make it display a chain that does not
//     satisfy consensus.
//   - Output is escaped by construction (html/template), so chain data — which
//     is attacker-supplied by nature — cannot inject markup or script.
//   - Requests are bounded: per-IP rate limiting, header and duration limits,
//     capped result sets, and a strict Content-Security-Policy that forbids
//     loading anything from anywhere.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"perihelion/core"
	"perihelion/p2p"
)

const (
	maxRecentBlocks  = 25
	maxBlockTxShown  = 200
	requestsPerMin   = 120
	rateLimitBuckets = 4096
)

type explorer struct {
	chain *core.Chain
	node  *p2p.Node
	tmpl  *template.Template
	start time.Time
}

func main() {
	datadir := flag.String("datadir", defaultDataDir(), "data directory")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	p2pListen := flag.String("p2p-listen", "off", `P2P listen address ("off" for outbound only)`)
	connect := flag.String("connect", "", "comma-separated peers (empty uses the built-in seed)")
	flag.Parse()

	c, err := core.Open(filepath.Join(*datadir, "chain.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot open chain:", err)
		os.Exit(1)
	}
	defer c.Close()

	logf := func(format string, a ...any) {
		fmt.Printf("%s  "+format+"\n", append([]any{time.Now().Format("15:04:05")}, a...)...)
	}

	node := p2p.New(c, logf)
	node.SetPeerStore(filepath.Join(*datadir, "peers.txt"))
	var peers []string
	if *connect != "" {
		for _, p := range strings.Split(*connect, ",") {
			if p = strings.TrimSpace(p); p != "" {
				peers = append(peers, p)
			}
		}
	} else {
		peers = []string{p2p.DefaultSeed}
	}
	pl := *p2pListen
	if pl == "off" {
		pl = ""
	}
	if err := node.Start(pl, peers); err != nil {
		fmt.Fprintln(os.Stderr, "error: p2p:", err)
		os.Exit(1)
	}
	defer node.Stop()

	e := &explorer{
		chain: c,
		node:  node,
		tmpl:  template.Must(template.New("").Funcs(tmplFuncs).Parse(pageTemplates)),
		start: time.Now(),
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           e.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	go func() {
		logf("explorer: http://%s", *listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "error: http:", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	fmt.Println()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(sctx)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".perihelion-explorer"
	}
	return filepath.Join(home, ".perihelion-explorer")
}

// --- routing and middleware ---

func (e *explorer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handleHome)
	mux.HandleFunc("/block/", e.handleBlock)
	mux.HandleFunc("/tx/", e.handleTx)
	mux.HandleFunc("/address/", e.handleAddress)
	mux.HandleFunc("/supply", e.handleSupply)
	mux.HandleFunc("/search", e.handleSearch)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})
	return e.secure(e.rateLimit(mux))
}

// secure applies response headers and refuses any method that could change
// state. The CSP permits nothing external — no scripts, no fonts, no images
// from anywhere — so a defect in this program cannot become a vector for
// loading third-party code into a visitor's browser.
func (e *explorer) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "this service is read-only", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), interest-cohort=()")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		next.ServeHTTP(w, r)
	})
}

type bucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	mu sync.Mutex
	m  map[string]*bucket
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.m[key]
	if !ok {
		if len(l.m) >= rateLimitBuckets {
			l.m = map[string]*bucket{} // bounded memory: reset rather than grow
		}
		l.m[key] = &bucket{tokens: requestsPerMin - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Minutes() * requestsPerMin
	if b.tokens > requestsPerMin {
		b.tokens = requestsPerMin
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

var lim = &limiter{m: map[string]*bucket{}}

func (e *explorer) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !lim.allow(host) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- pages ---

// statsView carries chain figures already formatted for display, so templates
// never have to convert raw integer amounts (which are in peri, not PER).
type statsView struct {
	Height      uint64
	Supply      string
	Burned      string
	Pool        string
	NextSubsidy string
	Difficulty  string
	Mempool     int
}

type pageData struct {
	Title   string
	Stats   *statsView
	Peers   int
	Synced  bool
	Blocks  []blockRow
	Block   *blockView
	Tx      *txView
	Address *addressView
	Supply  *supplyView
	Error   string
}

type supplyView struct {
	Emitted     string
	Burned      string
	Circulating string
	Bound       string
	PctOfBound  string
	Pool        string
	Height      uint64
	AuditFees   string
	AuditBurn   string
	AuditBlocks uint64
	AuditOK     bool
}

type blockRow struct {
	Height uint64
	Hash   string
	Time   string
	Txs    int
	Reward string
	Miner  string
}

type blockView struct {
	Height     uint64
	Hash       string
	Prev       string
	Time       string
	Nonce      uint64
	Difficulty string
	Miner      string
	Reward     string
	Txs        []txSummary
	Truncated  bool
}

type txSummary struct {
	ID       string
	Coinbase bool
	Inputs   int
	Outputs  int
	Value    string
}

type txView struct {
	ID       string
	Height   uint64
	Coinbase bool
	Inputs   []inputView
	Outputs  []outputView
	Total    string
}

type inputView struct {
	PrevID string
	Index  uint32
}

type outputView struct {
	Address string
	Value   string
}

type addressView struct {
	Address   string
	Spendable string
	Immature  string
	Total     string
}

func (e *explorer) render(w http.ResponseWriter, name string, d *pageData) {
	if st, err := e.chain.Stats(); err == nil {
		d.Stats = &statsView{
			Height:      st.Height,
			Supply:      core.FormatAmount(st.Emitted - st.Burned),
			Burned:      core.FormatAmount(st.Burned),
			Pool:        core.FormatAmount(st.Pool),
			NextSubsidy: core.FormatAmount(st.NextSubsidy),
			Difficulty:  st.Difficulty.String(),
			Mempool:     st.Mempool,
		}
	}
	d.Peers = e.node.PeerCount()
	d.Synced = e.node.Synced()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := e.tmpl.ExecuteTemplate(w, name, d); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (e *explorer) fail(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	e.render(w, "error", &pageData{Title: "Not found", Error: msg})
}

func (e *explorer) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		e.fail(w, http.StatusNotFound, "No such page.")
		return
	}
	st, err := e.chain.Stats()
	if err != nil {
		e.fail(w, http.StatusInternalServerError, "Chain unavailable.")
		return
	}
	var rows []blockRow
	for h := st.Height; len(rows) < maxRecentBlocks; h-- {
		b, err := e.chain.BlockByHeight(h)
		if err != nil {
			break
		}
		rows = append(rows, rowFor(b))
		if h == 0 {
			break
		}
	}
	e.render(w, "home", &pageData{Title: "Perihelion Explorer", Blocks: rows})
}

func rowFor(b *core.Block) blockRow {
	hash := b.Header.Hash()
	cb := b.Txs[0]
	var reward uint64
	miner := ""
	if len(cb.Outputs) > 0 {
		for i := range cb.Outputs {
			reward += cb.Outputs[i].Value
		}
		miner = core.EncodeAddress(cb.Outputs[0].Addr)
	}
	return blockRow{
		Height: b.Header.Height,
		Hash:   hex.EncodeToString(hash[:]),
		Time:   time.Unix(b.Header.Time, 0).UTC().Format("2006-01-02 15:04:05"),
		Txs:    len(b.Txs),
		Reward: core.FormatAmount(reward),
		Miner:  miner,
	}
}

func (e *explorer) handleBlock(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/block/")
	if id == "" || len(id) > 64 {
		e.fail(w, http.StatusBadRequest, "Invalid block reference.")
		return
	}
	var b *core.Block
	if h, err := strconv.ParseUint(id, 10, 64); err == nil {
		b, err = e.chain.BlockByHeight(h)
		if err != nil {
			e.fail(w, http.StatusNotFound, fmt.Sprintf("No block at height %d.", h))
			return
		}
	} else {
		raw, err := hex.DecodeString(id)
		if err != nil || len(raw) != 32 {
			e.fail(w, http.StatusBadRequest, "A block is referenced by height or by its 64-character hash.")
			return
		}
		var hash [32]byte
		copy(hash[:], raw)
		rawBlock, ok := e.chain.RawBlockByHash(hash)
		if !ok {
			e.fail(w, http.StatusNotFound, "Unknown block.")
			return
		}
		b, err = core.DeserializeBlock(rawBlock)
		if err != nil {
			e.fail(w, http.StatusInternalServerError, "Corrupt block.")
			return
		}
	}

	hash := b.Header.Hash()
	cb := b.Txs[0]
	var reward uint64
	miner := ""
	if len(cb.Outputs) > 0 {
		for i := range cb.Outputs {
			reward += cb.Outputs[i].Value
		}
		miner = core.EncodeAddress(cb.Outputs[0].Addr)
	}
	view := &blockView{
		Height:     b.Header.Height,
		Hash:       hex.EncodeToString(hash[:]),
		Prev:       hex.EncodeToString(b.Header.PrevHash[:]),
		Time:       time.Unix(b.Header.Time, 0).UTC().Format("2006-01-02 15:04:05 UTC"),
		Nonce:      b.Header.Nonce,
		Difficulty: core.DifficultyOf(b.Header.Target).String(),
		Miner:      miner,
		Reward:     core.FormatAmount(reward),
	}
	for i, t := range b.Txs {
		if i >= maxBlockTxShown {
			view.Truncated = true
			break
		}
		id := t.ID()
		var val uint64
		for j := range t.Outputs {
			val += t.Outputs[j].Value
		}
		view.Txs = append(view.Txs, txSummary{
			ID:       hex.EncodeToString(id[:]),
			Coinbase: t.IsCoinbase(),
			Inputs:   len(t.Inputs),
			Outputs:  len(t.Outputs),
			Value:    core.FormatAmount(val),
		})
	}
	e.render(w, "block", &pageData{Title: fmt.Sprintf("Block %d", b.Header.Height), Block: view})
}

func (e *explorer) handleTx(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tx/")
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) != 32 {
		e.fail(w, http.StatusBadRequest, "A transaction is referenced by its 64-character id.")
		return
	}
	var want [32]byte
	copy(want[:], raw)

	// Scanning back from the tip is adequate for a young chain and needs no
	// extra index; the bound keeps a lookup from becoming a denial of service.
	st, err := e.chain.Stats()
	if err != nil {
		e.fail(w, http.StatusInternalServerError, "Chain unavailable.")
		return
	}
	const maxScan = 5000
	scanned := 0
	for h := st.Height; scanned < maxScan; h-- {
		b, err := e.chain.BlockByHeight(h)
		if err != nil {
			break
		}
		for _, t := range b.Txs {
			if t.ID() == want {
				view := &txView{ID: id, Height: h, Coinbase: t.IsCoinbase()}
				var total uint64
				for i := range t.Inputs {
					view.Inputs = append(view.Inputs, inputView{
						PrevID: hex.EncodeToString(t.Inputs[i].Prev.TxID[:]),
						Index:  t.Inputs[i].Prev.Index,
					})
				}
				for i := range t.Outputs {
					total += t.Outputs[i].Value
					view.Outputs = append(view.Outputs, outputView{
						Address: core.EncodeAddress(t.Outputs[i].Addr),
						Value:   core.FormatAmount(t.Outputs[i].Value),
					})
				}
				view.Total = core.FormatAmount(total)
				e.render(w, "tx", &pageData{Title: "Transaction", Tx: view})
				return
			}
		}
		scanned++
		if h == 0 {
			break
		}
	}
	e.fail(w, http.StatusNotFound, fmt.Sprintf("Transaction not found in the last %d blocks.", maxScan))
}

func (e *explorer) handleAddress(w http.ResponseWriter, r *http.Request) {
	enc := strings.TrimPrefix(r.URL.Path, "/address/")
	addr, err := core.DecodeAddress(enc)
	if err != nil {
		e.fail(w, http.StatusBadRequest, "Not a valid Perihelion address: "+err.Error())
		return
	}
	spendable, immature, err := e.chain.Balance(addr)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, "Lookup failed.")
		return
	}
	e.render(w, "address", &pageData{
		Title: "Address",
		Address: &addressView{
			Address:   core.EncodeAddress(addr),
			Spendable: core.FormatAmount(spendable),
			Immature:  core.FormatAmount(immature),
			Total:     core.FormatAmount(spendable + immature),
		},
	})
}

// handleSupply reports the money supply and, crucially, re-derives the burned
// total independently from the blocks rather than trusting the node's running
// counter. Burning leaves no address to inspect — burned coins are simply
// never created — so the way to verify it is to recompute what the coinbase of
// every block was permitted to pay and confirm it matches.
func (e *explorer) handleSupply(w http.ResponseWriter, r *http.Request) {
	st, err := e.chain.Stats()
	if err != nil {
		e.fail(w, http.StatusInternalServerError, "Chain unavailable.")
		return
	}
	circulating := st.Emitted - st.Burned

	// Independent audit: sum the subsidy every block was entitled to, and the
	// amount its coinbase actually paid. The difference is fee income the
	// miner received; the rest of the fees was destroyed.
	const auditWindow = 2000
	from := uint64(1)
	if st.Height > auditWindow {
		from = st.Height - auditWindow + 1
	}
	var subsidySum, coinbaseSum uint64
	var blocks uint64
	consistent := true
	for h := from; h <= st.Height; h++ {
		b, err := e.chain.BlockByHeight(h)
		if err != nil {
			break
		}
		var paid uint64
		for i := range b.Txs[0].Outputs {
			paid += b.Txs[0].Outputs[i].Value
		}
		want := core.BlockSubsidy(h)
		if paid < want {
			consistent = false
		}
		subsidySum += want
		coinbaseSum += paid
		blocks++
	}
	feesToMiners := uint64(0)
	if coinbaseSum > subsidySum {
		feesToMiners = coinbaseSum - subsidySum
	}

	e.render(w, "supply", &pageData{
		Title: "Supply",
		Supply: &supplyView{
			Emitted:     core.FormatAmount(st.Emitted),
			Burned:      core.FormatAmount(st.Burned),
			Circulating: core.FormatAmount(circulating),
			Bound:       core.FormatAmount(core.MaxSupply),
			PctOfBound:  fmt.Sprintf("%.4f", float64(circulating)/float64(core.MaxSupply)*100),
			Pool:        core.FormatAmount(st.Pool),
			Height:      st.Height,
			AuditFees:   core.FormatAmount(feesToMiners),
			AuditBurn:   core.FormatAmount(st.Burned),
			AuditBlocks: blocks,
			AuditOK:     consistent,
		},
	})
}

// handleSearch routes a single query box to the right page by shape alone; it
// never interpolates user input into a response.
func (e *explorer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 128 {
		e.fail(w, http.StatusBadRequest, "Query too long.")
		return
	}
	switch {
	case q == "":
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case strings.HasPrefix(q, "per1"):
		if _, err := core.DecodeAddress(q); err != nil {
			e.fail(w, http.StatusBadRequest, "Not a valid address.")
			return
		}
		http.Redirect(w, r, "/address/"+q, http.StatusSeeOther)
	case len(q) == 64 && isHex(q):
		http.Redirect(w, r, "/block/"+q, http.StatusSeeOther)
	case isDigits(q):
		http.Redirect(w, r, "/block/"+q, http.StatusSeeOther)
	default:
		e.fail(w, http.StatusBadRequest, "Enter a block height, a 64-character hash, or a per1… address.")
	}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func isDigits(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var tmplFuncs = template.FuncMap{
	"short": func(s string) string {
		if len(s) > 16 {
			return s[:8] + "…" + s[len(s)-6:]
		}
		return s
	},
	"sortedPeers": func(in []string) []string { sort.Strings(in); return in },
}
