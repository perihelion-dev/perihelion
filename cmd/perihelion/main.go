// Command perihelion is the all-in-one Perihelion node: wallet, miner and
// chain inspector. Run `perihelion help` for usage.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"perihelion/core"
	"perihelion/wallet"
)

// promptPassword reads a password from the terminal without echoing it.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Non-interactive (piped) input: read a line as fallback.
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimRight(line, "\r\n"), nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

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

// loadWalletAddr loads a wallet for read-only use (address/balance): it never
// needs the password because the public key is stored in clear.
func loadWalletAddr(datadir string) (*wallet.Wallet, error) {
	w, err := wallet.Load(walletPath(datadir))
	if err != nil {
		return nil, fmt.Errorf("no wallet found (%v) — create one with: perihelion wallet new", err)
	}
	return w, nil
}

// loadWallet loads and, if the wallet is encrypted, unlocks it by prompting
// for the password. Used by commands that must sign (send).
func loadWallet(datadir string) (*wallet.Wallet, error) {
	w, err := loadWalletAddr(datadir)
	if err != nil {
		return nil, err
	}
	if w.Locked() {
		pw, err := promptPassword("Wallet password: ")
		if err != nil {
			return nil, err
		}
		if err := w.Unlock(pw); err != nil {
			return nil, err
		}
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
	case "governance":
		err = cmdGovernance(os.Args[2:])
	case "miners":
		err = cmdMiners(os.Args[2:])
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
  perihelion wallet new                        create a quantum-safe wallet (password + 24-word phrase)
  perihelion wallet restore                     restore a wallet from its 24-word phrase
  perihelion wallet show                       show your address
  perihelion wallet phrase                      reveal your recovery phrase (asks for password)
  perihelion balance [ADDRESS]                 show balance
  perihelion mine [--blocks N]                 mine solo (default: until Ctrl-C)
  perihelion node [--connect HOST:PORT] [--mine]
                                               run a networked node (P2P + local RPC)
  perihelion send --to ADDR --amount PER [--fee PER]
  perihelion info                              chain statistics
  perihelion block HEIGHT                      inspect a block
  perihelion governance                        list proposed rule changes and their network status
  perihelion miners [--from N] [--to N]        who actually mined the blocks, by reward address

All commands accept --datadir DIR (default ~/.perihelion).
`)
}

func cmdGovernance(args []string) error {
	fs := flag.NewFlagSet("governance", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(core.Deployments) == 0 {
		fmt.Println("No rule changes are currently proposed.")
		fmt.Println()
		fmt.Println("Perihelion changes consensus rules only through miner signalling:")
		fmt.Printf("a proposal activates when %d of %d blocks in a window signal support\n", core.SignalThreshold, core.SignalWindow)
		fmt.Println("(90% over ~7 days), and takes effect one window later. Monetary policy")
		fmt.Println("(emission, supply bound, fee burn) is permanently outside this process.")
		return nil
	}
	c, err := openChain(*datadir)
	if err != nil {
		return err
	}
	defer c.Close()
	for _, d := range core.Deployments {
		st, err := c.DeploymentStatus(d)
		if err != nil {
			return err
		}
		fmt.Printf("%-24s bit %-2d  state %-10s", d.Name, d.Bit, st.State)
		if st.State == core.StateStarted && st.WindowBlocks > 0 {
			fmt.Printf("  window %d: %d/%d signalling", st.WindowStart, st.WindowSignals, st.WindowBlocks)
		}
		if st.ActivationHeight > 0 {
			fmt.Printf("  activation height %d", st.ActivationHeight)
		}
		fmt.Println()
	}
	return nil
}

// cmdMiners attributes blocks to the addresses their coinbase paid. This is
// the only sound way to ask who mined: the peer a block arrived from merely
// relayed it, and relaying says nothing about who found it.
func cmdMiners(args []string) error {
	fs := flag.NewFlagSet("miners", flag.ExitOnError)
	datadir := fs.String("datadir", defaultDataDir(), "data directory")
	from := fs.Uint64("from", 1, "first height to examine")
	to := fs.Uint64("to", 0, "last height to examine (0 = chain tip)")
	if err := fs.Parse(args); err != nil {
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
	last := *to
	if last == 0 || last > st.Height {
		last = st.Height
	}
	if *from < 1 {
		*from = 1
	}

	counts := map[[32]byte]uint64{}
	rewards := map[[32]byte]uint64{}
	var total uint64
	for h := *from; h <= last; h++ {
		b, err := c.BlockByHeight(h)
		if err != nil {
			return err
		}
		cb := b.Txs[0]
		if len(cb.Outputs) == 0 {
			continue
		}
		addr := cb.Outputs[0].Addr
		var paid uint64
		for i := range cb.Outputs {
			paid += cb.Outputs[i].Value
		}
		counts[addr]++
		rewards[addr] += paid
		total++
	}
	if total == 0 {
		fmt.Println("No blocks in that range.")
		return nil
	}

	type row struct {
		addr   [32]byte
		blocks uint64
		reward uint64
	}
	rows := make([]row, 0, len(counts))
	for a, n := range counts {
		rows = append(rows, row{a, n, rewards[a]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].blocks > rows[j].blocks })

	var mine [32]byte
	haveWallet := false
	if w, err := loadWalletAddr(*datadir); err == nil {
		mine, haveWallet = w.Address(), true
	}

	fmt.Printf("Blocks %d–%d: %d blocks, %d distinct miners\n\n", *from, last, total, len(rows))
	fmt.Printf("%-8s %-7s %-16s %s\n", "BLOCKS", "SHARE", "REWARDED", "ADDRESS")
	for _, r := range rows {
		tag := ""
		if haveWallet && r.addr == mine {
			tag = "  ← you"
		}
		fmt.Printf("%-8d %-7s %-16s %s%s\n",
			r.blocks,
			fmt.Sprintf("%.1f%%", float64(r.blocks)*100/float64(total)),
			core.FormatAmount(r.reward)+" PER",
			wallet.EncodeAddress(r.addr), tag)
	}
	// A single party holding a majority of recent blocks can rewrite recent
	// history; say so rather than leaving the reader to notice.
	if len(rows) > 0 && float64(rows[0].blocks)*100/float64(total) > 50 {
		fmt.Printf("\nWARNING: one address mined %.1f%% of this range. A majority of hashrate\n",
			float64(rows[0].blocks)*100/float64(total))
		fmt.Println("can reorganise recent blocks. Treat confirmations here with caution.")
	}
	return nil
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
		pw, err := newPassword()
		if err != nil {
			return err
		}
		w, mnemonic, err := wallet.Create(pw)
		if err != nil {
			return err
		}
		if err := w.Save(path); err != nil {
			return err
		}
		fmt.Println("\nNew quantum-safe wallet created (ML-DSA-65).")
		fmt.Println("Address:", wallet.EncodeAddress(w.Address()))
		printMnemonic(mnemonic)
		return nil
	case "restore":
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("wallet already exists at %s — move it aside before restoring", path)
		}
		fmt.Println("Enter your 24-word recovery phrase (all on one line):")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		pw, err := newPassword()
		if err != nil {
			return err
		}
		w, err := wallet.Restore(line, pw)
		if err != nil {
			return err
		}
		if err := w.Save(path); err != nil {
			return err
		}
		fmt.Println("\nWallet restored.")
		fmt.Println("Address:", wallet.EncodeAddress(w.Address()))
		return nil
	case "show":
		w, err := loadWalletAddr(*datadir)
		if err != nil {
			return err
		}
		fmt.Println("Address:", wallet.EncodeAddress(w.Address()))
		fmt.Println("Scheme: ", core.SigScheme.Name())
		return nil
	case "phrase":
		w, err := loadWalletAddr(*datadir)
		if err != nil {
			return err
		}
		pw, err := promptPassword("Wallet password: ")
		if err != nil {
			return err
		}
		m, err := w.RevealMnemonic(pw)
		if err != nil {
			return err
		}
		printMnemonic(m)
		return nil
	default:
		return fmt.Errorf("usage: perihelion wallet new|restore|show|phrase")
	}
}

// newPassword prompts for a password twice and checks they match.
func newPassword() (string, error) {
	pw, err := promptPassword("Choose a wallet password: ")
	if err != nil {
		return "", err
	}
	if len(pw) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	again, err := promptPassword("Repeat password: ")
	if err != nil {
		return "", err
	}
	if pw != again {
		return "", fmt.Errorf("passwords do not match")
	}
	return pw, nil
}

func printMnemonic(m string) {
	fmt.Println()
	fmt.Println("┌─ RECOVERY PHRASE ─ write these 24 words down and keep them offline ─┐")
	words := strings.Fields(m)
	for i := 0; i < len(words); i += 4 {
		fmt.Print("  ")
		for j := i; j < i+4 && j < len(words); j++ {
			fmt.Printf("%2d. %-10s", j+1, words[j])
		}
		fmt.Println()
	}
	fmt.Println("└────────────────────────────────────────────────────────────────────┘")
	fmt.Println("Anyone with these words controls your coins. We can never recover them for you.")
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
		w, err := loadWalletAddr(*datadir)
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
	threads := fs.Int("threads", 0, "mining threads (0 = automatic)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	core.SetMinerThreads(*threads)
	w, err := loadWalletAddr(*datadir)
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
	memo := fs.String("memo", "", "public payment reference recorded in the transaction (invoice or order id; max 80 bytes)")
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
	tx, err := wallet.BuildSendWithRef(c, w, toAddr, amount, fee, []byte(*memo))
	if err != nil {
		return err
	}
	if err := c.SubmitTx(tx); err != nil {
		return err
	}
	id := tx.ID()
	fmt.Printf("Transaction accepted: %x\n", id[:])
	if *memo != "" {
		fmt.Printf("Reference (public, permanent): %s\n", *memo)
	}
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
