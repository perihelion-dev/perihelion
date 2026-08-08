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
	listen := fs.String("listen", fmt.Sprintf(":%d", p2p.DefaultPort), `P2P listen address ("off" to disable)`)
	connect := fs.String("connect", "", "comma-separated peer addresses (host:port)")
	mine := fs.Bool("mine", false, "mine while running the node")
	rpcAddr := fs.String("rpc", "127.0.0.1:16181", `local RPC address ("off" to disable)`)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()

	var w *wallet.Wallet
	if wl, err := wallet.Load(walletPath(*datadir)); err == nil {
		w = wl
	} else if *mine {
		return fmt.Errorf("mining needs a wallet — create one with: perihelion wallet new")
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
	if *connect != "" {
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
		rpcServer = startRPC(*rpcAddr, token, c, w, node, logf)
		logf("rpc: http://%s (token in %s)", *rpcAddr, filepath.Join(*datadir, "rpc-token"))
	}

	if *mine {
		go func() {
			if err := core.MineLoop(ctx, c, w.Address(), 0, core.MineOpts{
				Logf:    logf,
				OnBlock: node.BroadcastBlock,
			}); err != nil {
				logf("miner stopped: %v", err)
			}
		}()
		logf("miner: %d threads, rewards to %s", core.MinerThreads(), wallet.EncodeAddress(w.Address()))
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

// startRPC serves a small authenticated JSON API on localhost: /status,
// /balance and /send. This is the machine interface — an AI agent or script
// talks to the node exactly the way the CLI does, no browser, no cloud.
func startRPC(addr, token string, c *core.Chain, w *wallet.Wallet, node *p2p.Node, logf func(string, ...any)) *http.Server {
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
		logf("rpc: sent %s PER to %s (tx %x…)", core.FormatAmount(amount), req.To[:12], id[:8])
		writeJSON(rw, map[string]any{"txid": hex.EncodeToString(id[:])})
	}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logf("rpc error: %v", err)
		}
	}()
	return srv
}
