package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"perihelion/core"
	"perihelion/p2p"
	"perihelion/wallet"
)

func cmdNode(args []string) error {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	listen := fs.String("listen", fmt.Sprintf(":%d", p2p.DefaultPort), `P2P listen address ("off" to disable — outbound connections only)`)
	connect := fs.String("connect", "", `comma-separated peer addresses (host:port); empty uses ~/.perihelion/seeds.txt or the built-in seed, "none" disables outbound`)
	mine := fs.Bool("mine", false, "mine while running the node")
	mineto := fs.String("mineto", "", "mine to this address instead of the local wallet (per1...); lets a server mine without holding any keys")
	threads := fs.Int("threads", 0, "mining threads (0 = automatic)")
	rpcAddr := fs.String("rpc", "127.0.0.1:16181", `local RPC + dashboard address ("off" to disable)`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	core.SetMinerThreads(*threads)

	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()

	var w *wallet.Wallet
	if wl, err := wallet.Load(walletPath(*datadir)); err == nil {
		w = wl
	}

	var mineAddr [32]byte
	if *mine {
		switch {
		case *mineto != "":
			a, err := wallet.DecodeAddress(*mineto)
			if err != nil {
				return fmt.Errorf("--mineto: %w", err)
			}
			mineAddr = a
		case w != nil:
			mineAddr = w.Address()
		default:
			return fmt.Errorf("mining needs a wallet (perihelion wallet new) or an explicit --mineto address")
		}
	}

	logf := func(format string, a ...any) {
		fmt.Printf("%s  "+format+"\n", append([]any{time.Now().Format("15:04:05")}, a...)...)
	}

	node := p2p.New(c, logf)
	listenAddr := *listen
	if listenAddr == "off" {
		listenAddr = ""
	}
	var peers []string
	switch *connect {
	case "none":
		// no outbound connections
	case "":
		peers = seedPeers(*datadir)
	default:
		for _, pa := range strings.Split(*connect, ",") {
			if pa = strings.TrimSpace(pa); pa != "" {
				peers = append(peers, pa)
			}
		}
	}
	if err := node.Start(listenAddr, peers); err != nil {
		return err
	}
	defer node.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var rpcServer *http.Server
	if *rpcAddr != "off" {
		if !strings.HasPrefix(*rpcAddr, "127.0.0.1:") && !strings.HasPrefix(*rpcAddr, "localhost:") {
			logf("WARNING: RPC on %s is reachable from the network — keep it on 127.0.0.1 unless you know exactly what you are doing", *rpcAddr)
		}
		token, err := rpcToken(*datadir)
		if err != nil {
			return err
		}
		rpcServer = startRPC(*rpcAddr, token, c, w, node, *mine, logf)
		logf("dashboard: http://%s", *rpcAddr)
	}

	if *mine {
		go func() {
			if err := core.MineLoop(ctx, c, mineAddr, 0, core.MineOpts{
				Logf:    logf,
				OnBlock: node.BroadcastBlock,
			}); err != nil {
				logf("miner stopped: %v", err)
			}
		}()
		logf("miner: %d threads, rewards to %s", core.MinerThreads(), wallet.EncodeAddress(mineAddr))
	}

	if st, err := c.Stats(); err == nil {
		logf("chain: height %d, supply %s PER", st.Height, core.FormatAmount(st.Emitted-st.Burned))
	}
	logf("node running — Ctrl-C stops")
	<-ctx.Done()
	fmt.Println()
	if rpcServer != nil {
		sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
		rpcServer.Shutdown(sctx)
		scancel()
	}
	return nil
}

// seedPeers returns the bootstrap peers: the operator's own seeds.txt if
// present (one host:port per line, # comments), otherwise the built-in seed.
func seedPeers(datadir string) []string {
	var out []string
	if data, err := os.ReadFile(filepath.Join(datadir, "seeds.txt")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		out = append(out, p2p.DefaultSeed)
	}
	return out
}

// rpcToken loads or creates the shared-secret token protecting the local RPC.
func rpcToken(datadir string) (string, error) {
	path := filepath.Join(datadir, "rpc-token")
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(datadir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// startRPC serves the dashboard and a small authenticated JSON API on
// localhost: /status, /balance, /recent and /send. This is the machine
// interface — an AI agent or script talks to the node exactly the way the
// dashboard does. The Host check blocks DNS-rebinding tricks; the token
// blocks blind cross-site requests.
func startRPC(addr, token string, c *core.Chain, w *wallet.Wallet, node *p2p.Node, mining bool, logf func(string, ...any)) *http.Server {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(rw http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Auth")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(rw, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(rw, r)
		}
	}
	writeJSON := func(rw http.ResponseWriter, v any) {
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(v)
	}
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(rw, strings.ReplaceAll(dashboardHTML, "__TOKEN__", token))
	})
	mux.HandleFunc("/status", auth(func(rw http.ResponseWriter, r *http.Request) {
		st, err := c.Stats()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(rw, map[string]any{
			"height":     st.Height,
			"tip":        hex.EncodeToString(st.TipHash[:]),
			"supply":     core.FormatAmount(st.Emitted - st.Burned),
			"emitted":    core.FormatAmount(st.Emitted),
			"burned":     core.FormatAmount(st.Burned),
			"pool":       core.FormatAmount(st.Pool),
			"difficulty": st.Difficulty.String(),
			"mempool":    st.Mempool,
			"peers":      node.PeerCount(),
			"mining":     mining,
			"threads":    core.MinerThreads(),
		})
	}))
	mux.HandleFunc("/balance", auth(func(rw http.ResponseWriter, r *http.Request) {
		if w == nil {
			http.Error(rw, "no wallet loaded", http.StatusBadRequest)
			return
		}
		sp, im, err := c.Balance(w.Address())
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(rw, map[string]any{
			"address":   wallet.EncodeAddress(w.Address()),
			"spendable": core.FormatAmount(sp),
			"immature":  core.FormatAmount(im),
		})
	}))
	mux.HandleFunc("/recent", auth(func(rw http.ResponseWriter, r *http.Request) {
		st, err := c.Stats()
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		blocks := []map[string]any{}
		for h := st.Height; h > 0 && len(blocks) < 15; h-- {
			b, err := c.BlockByHeight(h)
			if err != nil {
				break
			}
			cb := b.Txs[0]
			var reward uint64
			for i := range cb.Outputs {
				reward += cb.Outputs[i].Value
			}
			mine := w != nil && len(cb.Outputs) > 0 && cb.Outputs[0].Addr == w.Address()
			bh := b.Header.Hash()
			blocks = append(blocks, map[string]any{
				"height": h,
				"hash":   hex.EncodeToString(bh[:8]),
				"time":   b.Header.Time,
				"txs":    len(b.Txs),
				"reward": core.FormatAmount(reward),
				"mine":   mine,
			})
		}
		writeJSON(rw, map[string]any{"blocks": blocks})
	}))
	mux.HandleFunc("/send", auth(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if w == nil {
			http.Error(rw, "no wallet loaded", http.StatusBadRequest)
			return
		}
		var req struct{ To, Amount, Fee string }
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 4096)).Decode(&req); err != nil {
			http.Error(rw, "bad json", http.StatusBadRequest)
			return
		}
		to, err := wallet.DecodeAddress(req.To)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		amount, err := core.ParseAmount(req.Amount)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Fee == "" {
			req.Fee = "0.001"
		}
		fee, err := core.ParseAmount(req.Fee)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		tx, err := wallet.BuildSend(c, w, to, amount, fee)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		if err := c.SubmitTx(tx); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		node.BroadcastTx(tx)
		id := tx.ID()
		logf("rpc: sent %s PER to %s… (tx %x…)", core.FormatAmount(amount), req.To[:12], id[:8])
		writeJSON(rw, map[string]any{"txid": hex.EncodeToString(id[:])})
	}))

	// Host allowlist (only when bound to loopback): defeats DNS-rebinding.
	var handler http.Handler = mux
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
		handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			host := r.Host
			if i := strings.LastIndex(host, ":"); i >= 0 {
				host = host[:i]
			}
			if host != "127.0.0.1" && host != "localhost" {
				http.Error(rw, "forbidden host", http.StatusForbidden)
				return
			}
			mux.ServeHTTP(rw, r)
		})
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logf("rpc error: %v", err)
		}
	}()
	return srv
}
