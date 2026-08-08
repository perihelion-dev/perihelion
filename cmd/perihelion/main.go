// Command perihelion is the all-in-one Perihelion node: wallet, miner and
// chain inspector. Run `perihelion help` for usage.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"perihelion/core"
	"perihelion/wallet"
)

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".perihelion"
	}
	return filepath.Join(home, ".perihelion")
}

func openChain(datadir string) (*core.Chain, error) {
	c, err := core.Open(filepath.Join(datadir, "chain.db"))
	if err != nil && strings.Contains(err.Error(), "timeout") {
		return nil, fmt.Errorf("chain database is locked — a perihelion node is probably running on this datadir; stop it or use its RPC instead (%w)", err)
	}
	return c, err
}

func walletPath(datadir string) string {
	return filepath.Join(datadir, "wallet.json")
}

func loadWallet(datadir string) (*wallet.Wallet, error) {
	w, err := wallet.Load(walletPath(datadir))
	if err != nil {
		return nil, fmt.Errorf("no wallet found (%v) — create one with: perihelion wallet new", err)
	}
	return w, nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "wallet":
		err = cmdWallet(os.Args[2:])
	case "balance":
		err = cmdBalance(os.Args[2:])
	case "mine":
		err = cmdMine(os.Args[2:])
	case "node":
		err = cmdNode(os.Args[2:])
	case "send":
		err = cmdSend(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "block":
		err = cmdBlock(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Perihelion (PER) — post-quantum, CPU-mineable, deflationary.

Usage:
  perihelion wallet new                        create a quantum-safe wallet
  perihelion wallet show                       show your address
  perihelion balance [ADDRESS]                 show balance
  perihelion mine [--blocks N]                 mine solo (default: until Ctrl-C)
  perihelion node [--connect HOST:PORT] [--mine]
                                               run a networked node (P2P + local RPC)
  perihelion send --to ADDR --amount PER [--fee PER]
  perihelion info                              chain statistics
  perihelion block HEIGHT                      inspect a block

All commands accept --datadir DIR (default ~/.perihelion).
`)
}

func cmdWallet(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: perihelion wallet new|show")
	}
	sub := args[0]
	fs := flag.NewFlagSet("wallet", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	path := walletPath(*datadir)
	switch sub {
	case "new":
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("wallet already exists at %s — refusing to overwrite it", path)
		}
		w, err := wallet.New()
		if err != nil {
			return err
		}
		if err := w.Save(path); err != nil {
			return err
		}
		fmt.Println("New quantum-safe wallet created (ML-DSA-65).")
		fmt.Println("Address:", wallet.EncodeAddress(w.Address()))
		fmt.Println()
		fmt.Println("IMPORTANT: back up this file — it IS your money:")
		fmt.Println(" ", path)
		return nil
	case "show":
		w, err := loadWallet(*datadir)
		if err != nil {
			return err
		}
		fmt.Println("Address:", wallet.EncodeAddress(w.Address()))
		fmt.Println("Scheme: ", core.SigScheme.Name())
		return nil
	default:
		return fmt.Errorf("usage: perihelion wallet new|show")
	}
}

func cmdBalance(args []string) error {
	fs := flag.NewFlagSet("balance", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var addr [32]byte
	if fs.NArg() > 0 {
		a, err := wallet.DecodeAddress(fs.Arg(0))
		if err != nil {
			return err
		}
		addr = a
	} else {
		w, err := loadWallet(*datadir)
		if err != nil {
			return err
		}
		addr = w.Address()
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	spendable, immature, err := c.Balance(addr)
	if err != nil {
		return err
	}
	fmt.Printf("Spendable: %s PER\n", core.FormatAmount(spendable))
	if immature > 0 {
		fmt.Printf("Immature:  %s PER (mining rewards unlock after %d blocks)\n",
			core.FormatAmount(immature), core.CoinbaseMaturity)
	}
	return nil
}

func cmdMine(args []string) error {
	fs := flag.NewFlagSet("mine", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	blocks := fs.Int("blocks", 0, "number of blocks to mine (0 = until Ctrl-C)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := loadWallet(*datadir)
	if err != nil {
		return err
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	st, err := c.Stats()
	if err != nil {
		return err
	}
	fmt.Printf("Mining to %s\n", wallet.EncodeAddress(w.Address()))
	fmt.Printf("Height %d, difficulty %s, %d threads (Argon2id, %d MiB each). Ctrl-C stops.\n",
		st.Height, st.Difficulty, core.MinerThreads(), core.PowMemoryKiB/1024)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = core.Mine(ctx, c, w.Address(), *blocks, func(format string, a ...any) {
		fmt.Printf(format+"\n", a...)
	})
	if err != nil {
		return err
	}
	fmt.Println("Mining stopped.")
	return nil
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	to := fs.String("to", "", "recipient address (per1...)")
	amountStr := fs.String("amount", "", "amount in PER, e.g. 1.5")
	feeStr := fs.String("fee", "0.001", "fee in PER (half is burned, half pays miners)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *to == "" || *amountStr == "" {
		return fmt.Errorf("usage: perihelion send --to per1... --amount 1.5 [--fee 0.001]")
	}
	toAddr, err := wallet.DecodeAddress(*to)
	if err != nil {
		return err
	}
	amount, err := core.ParseAmount(*amountStr)
	if err != nil {
		return err
	}
	fee, err := core.ParseAmount(*feeStr)
	if err != nil {
		return err
	}
	w, err := loadWallet(*datadir)
	if err != nil {
		return err
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	tx, err := wallet.BuildSend(c, w, toAddr, amount, fee)
	if err != nil {
		return err
	}
	if err := c.SubmitTx(tx); err != nil {
		return err
	}
	id := tx.ID()
	fmt.Printf("Transaction accepted: %x\n", id[:])
	fmt.Printf("Sending %s PER (fee %s: %s burned forever, %s to the miner pool).\n",
		core.FormatAmount(amount), core.FormatAmount(fee),
		core.FormatAmount(fee/2), core.FormatAmount(fee-fee/2))
	fmt.Println("It confirms with the next mined block.")
	return nil
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	s, err := c.Stats()
	if err != nil {
		return err
	}
	fmt.Printf("Height:        %d\n", s.Height)
	fmt.Printf("Tip:           %x\n", s.TipHash[:])
	fmt.Printf("Tip time:      %s\n", time.Unix(s.TipTime, 0).Format(time.RFC3339))
	fmt.Printf("Difficulty:    %s\n", s.Difficulty)
	fmt.Printf("Supply:        %s PER (emitted %s − burned %s)\n",
		core.FormatAmount(s.Emitted-s.Burned), core.FormatAmount(s.Emitted), core.FormatAmount(s.Burned))
	fmt.Printf("Miner pool:    %s PER (next payout ≈ %s)\n",
		core.FormatAmount(s.Pool), core.FormatAmount(s.NextPayout))
	fmt.Printf("Next subsidy:  %s PER\n", core.FormatAmount(s.NextSubsidy))
	fmt.Printf("Mempool:       %d transaction(s)\n", s.Mempool)
	return nil
}

func cmdBlock(args []string) error {
	fs := flag.NewFlagSet("block", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: perihelion block HEIGHT")
	}
	height, err := strconv.ParseUint(fs.Arg(0), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid height %q", fs.Arg(0))
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	b, err := c.BlockByHeight(height)
	if err != nil {
		return err
	}
	h := b.Header.Hash()
	fmt.Printf("Block %d\n", b.Header.Height)
	fmt.Printf("Hash:   %x\n", h[:])
	fmt.Printf("Prev:   %x\n", b.Header.PrevHash[:])
	fmt.Printf("Time:   %s\n", time.Unix(b.Header.Time, 0).Format(time.RFC3339))
	fmt.Printf("Nonce:  %d\n", b.Header.Nonce)
	fmt.Printf("Txs:    %d\n", len(b.Txs))
	for i, t := range b.Txs {
		id := t.ID()
		kind := ""
		if t.IsCoinbase() {
			kind = " (coinbase)"
		}
		var out uint64
		for j := range t.Outputs {
			out += t.Outputs[j].Value
		}
		fmt.Printf("  %d. %x  %s PER%s\n", i, id[:8], core.FormatAmount(out), kind)
	}
	return nil
}
