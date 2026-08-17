// Command perihelion-app is a native desktop wallet & miner for macOS,
// Windows and Linux, built with Fyne. It embeds a full Perihelion node: it
// mines, syncs with the seed peers, and lets you see your balance and send
// PER — all in one window, no browser, no cloud.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"perihelion/core"
	"perihelion/p2p"
	"perihelion/wallet"
)

func dataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".perihelion"
	}
	return filepath.Join(home, ".perihelion")
}

func walletPath() string { return filepath.Join(dataDir(), "wallet.json") }

// readSeeds returns the configured seed peers. It reads ~/.perihelion/seeds.txt
// (one host:port per line, # for comments); if absent it falls back to the
// built-in default so a fresh install still finds the network.
func readSeeds() []string {
	var out []string
	if data, err := os.ReadFile(filepath.Join(dataDir(), "seeds.txt")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		out = append(out, defaultSeeds()...)
	}
	return out
}

// defaultSeeds are where a fresh install looks for the network.
func defaultSeeds() []string { return p2p.DefaultSeeds() }

type appState struct {
	win    fyne.Window
	chain  *core.Chain
	node   *p2p.Node
	wallet *wallet.Wallet
	cancel context.CancelFunc

	mineCancel context.CancelFunc // nil while mining is stopped
	intensity  string
}

// Mining intensity levels. Mining is memory-hard by design: each thread holds
// 64 MiB and keeps a core busy continuously, so on a laptop the default is
// deliberately not "every core". "Sparsam" leaves the machine cool and quiet.
var intensityLevels = []string{"Aus", "Sparsam", "Mittel", "Voll"}

// threadsFor maps an intensity level to a worker count for this machine.
func threadsFor(level string) int {
	cores := runtime.NumCPU()
	switch level {
	case "Aus":
		return 0
	case "Sparsam":
		return max(1, cores/4)
	case "Mittel":
		return max(1, cores/2)
	default:
		return cores
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type appConfig struct {
	Intensity string `json:"intensity"`
}

func configPath() string { return filepath.Join(dataDir(), "app-config.json") }

func loadConfig() appConfig {
	cfg := appConfig{Intensity: "Sparsam"} // conservative default
	if data, err := os.ReadFile(configPath()); err == nil {
		var c appConfig
		if json.Unmarshal(data, &c) == nil {
			for _, l := range intensityLevels {
				if c.Intensity == l {
					cfg.Intensity = c.Intensity
				}
			}
		}
	}
	return cfg
}

func saveConfig(cfg appConfig) {
	if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		os.MkdirAll(dataDir(), 0o700)
		os.WriteFile(configPath(), data, 0o600)
	}
}

func main() {
	a := app.NewWithID("com.perihelion.wallet")
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("Perihelion Wallet")
	w.Resize(fyne.NewSize(560, 640))

	st := &appState{win: w}
	c, err := core.Open(filepath.Join(dataDir(), "chain.db"))
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not open chain: %w\n\nIs 'perihelion node' already running? Close it first — the app runs its own node.", err), w)
		w.ShowAndRun()
		return
	}
	st.chain = c

	if _, err := os.Stat(walletPath()); err == nil {
		st.showUnlock()
	} else {
		st.showOnboard()
	}
	w.SetCloseIntercept(func() {
		if st.cancel != nil {
			st.cancel()
		}
		c.Close()
		w.Close()
	})
	w.ShowAndRun()
}

// --- onboarding: create or restore ---

func (s *appState) showOnboard() {
	title := widget.NewLabelWithStyle("Willkommen bei Perihelion", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	intro := widget.NewLabel("Erstelle eine neue quantensichere Wallet oder stelle eine bestehende\nüber deine 24-Wörter-Phrase wieder her.")
	intro.Alignment = fyne.TextAlignCenter

	createBtn := widget.NewButtonWithIcon("Neue Wallet erstellen", theme.ContentAddIcon(), s.showCreate)
	createBtn.Importance = widget.HighImportance
	restoreBtn := widget.NewButtonWithIcon("Aus Phrase wiederherstellen", theme.HistoryIcon(), s.showRestore)

	s.win.SetContent(container.NewCenter(container.NewVBox(
		title, intro, widget.NewSeparator(), createBtn, restoreBtn,
	)))
}

func (s *appState) showCreate() {
	pw := widget.NewPasswordEntry()
	pw.SetPlaceHolder("Passwort (mind. 8 Zeichen)")
	pw2 := widget.NewPasswordEntry()
	pw2.SetPlaceHolder("Passwort wiederholen")

	form := container.NewVBox(
		widget.NewLabelWithStyle("Wallet-Passwort wählen", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Das Passwort verschlüsselt deine Wallet auf dieser Festplatte."),
		pw, pw2,
	)
	confirm := widget.NewButtonWithIcon("Erstellen", theme.ConfirmIcon(), func() {
		if len(pw.Text) < 8 {
			dialog.ShowError(fmt.Errorf("Passwort muss mindestens 8 Zeichen haben"), s.win)
			return
		}
		if pw.Text != pw2.Text {
			dialog.ShowError(fmt.Errorf("Passwörter stimmen nicht überein"), s.win)
			return
		}
		wl, mnemonic, err := wallet.Create(pw.Text)
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		if err := wl.Save(walletPath()); err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		s.wallet = wl
		s.showMnemonic(mnemonic)
	})
	confirm.Importance = widget.HighImportance
	back := widget.NewButton("Zurück", s.showOnboard)
	s.win.SetContent(container.NewCenter(container.NewVBox(form, confirm, back)))
}

func (s *appState) showMnemonic(mnemonic string) {
	warn := widget.NewLabelWithStyle("Deine Wiederherstellungs-Phrase", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	sub := widget.NewLabel("Schreibe diese 24 Wörter auf Papier und bewahre sie offline auf.\nWer sie kennt, besitzt deine Coins. Wir können sie NIE für dich wiederherstellen.")
	sub.Alignment = fyne.TextAlignCenter

	words := strings.Fields(mnemonic)
	grid := container.NewGridWithColumns(3)
	for i, wd := range words {
		grid.Add(widget.NewLabel(fmt.Sprintf("%2d. %s", i+1, wd)))
	}
	copyBtn := widget.NewButtonWithIcon("Kopieren (unsicher)", theme.ContentCopyIcon(), func() {
		s.copyMnemonicWithWarning(mnemonic)
	})
	confirmChk := widget.NewCheck("Ich habe die 24 Wörter sicher notiert", nil)
	cont := widget.NewButtonWithIcon("Weiter zur Wallet", theme.NavigateNextIcon(), func() {
		if !confirmChk.Checked {
			dialog.ShowInformation("Bitte bestätigen", "Bestätige zuerst, dass du die Phrase notiert hast.", s.win)
			return
		}
		s.startNodeAndDashboard()
	})
	cont.Importance = widget.HighImportance
	s.win.SetContent(container.NewVScroll(container.NewVBox(
		warn, sub, widget.NewCard("", "", grid), copyBtn, widget.NewSeparator(), confirmChk, cont,
	)))
}

// clipboardHoldSeconds is how long a copied recovery phrase is allowed to
// remain on the system clipboard before the app overwrites it.
const clipboardHoldSeconds = 45

// copyMnemonicWithWarning puts the recovery phrase on the clipboard only after
// the user acknowledges the risk, and clears it again shortly afterwards. The
// clipboard is readable by every other program on the machine, is retained by
// clipboard-history tools, and on macOS may be synced to other devices — so
// this is deliberately a warned, temporary, second-choice path. Writing the
// words down on paper remains the recommended backup.
func (s *appState) copyMnemonicWithWarning(mnemonic string) {
	dialog.ShowConfirm("Wirklich kopieren?",
		fmt.Sprintf("Die Zwischenablage kann von jedem anderen Programm auf diesem\n"+
			"Computer gelesen werden — auch von Schadsoftware — und wird von\n"+
			"macOS unter Umständen auf deine anderen Apple-Geräte übertragen.\n\n"+
			"Wer diese 24 Wörter erhält, besitzt dein gesamtes Guthaben.\n\n"+
			"Sicherer ist, sie auf Papier zu schreiben. Beim Kopieren wird die\n"+
			"Zwischenablage nach %d Sekunden automatisch überschrieben.",
			clipboardHoldSeconds),
		func(ok bool) {
			if !ok {
				return
			}
			cb := s.win.Clipboard()
			cb.SetContent(mnemonic)
			go func() {
				time.Sleep(clipboardHoldSeconds * time.Second)
				fyne.Do(func() {
					if cb.Content() == mnemonic {
						cb.SetContent("")
					}
				})
			}()
		}, s.win)
}

func (s *appState) showRestore() {
	phrase := widget.NewMultiLineEntry()
	phrase.SetPlaceHolder("Die 24 Wörter, durch Leerzeichen getrennt")
	phrase.SetMinRowsVisible(3)
	pw := widget.NewPasswordEntry()
	pw.SetPlaceHolder("Neues Passwort (mind. 8 Zeichen)")

	confirm := widget.NewButtonWithIcon("Wiederherstellen", theme.ConfirmIcon(), func() {
		if len(pw.Text) < 8 {
			dialog.ShowError(fmt.Errorf("Passwort muss mindestens 8 Zeichen haben"), s.win)
			return
		}
		wl, err := wallet.Restore(phrase.Text, pw.Text)
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		if err := wl.Save(walletPath()); err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		s.wallet = wl
		s.startNodeAndDashboard()
	})
	confirm.Importance = widget.HighImportance
	back := widget.NewButton("Zurück", s.showOnboard)
	s.win.SetContent(container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle("Wallet wiederherstellen", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		phrase, pw, confirm, back,
	)))
}

// --- unlock existing wallet ---

func (s *appState) showUnlock() {
	wl, err := wallet.Load(walletPath())
	if err != nil {
		dialog.ShowError(fmt.Errorf("wallet konnte nicht geladen werden: %w", err), s.win)
		return
	}
	s.wallet = wl
	if !wl.Locked() {
		s.startNodeAndDashboard()
		return
	}
	pw := widget.NewPasswordEntry()
	pw.SetPlaceHolder("Wallet-Passwort")
	unlock := widget.NewButtonWithIcon("Entsperren", theme.ConfirmIcon(), func() {
		if err := wl.Unlock(pw.Text); err != nil {
			dialog.ShowError(fmt.Errorf("Falsches Passwort"), s.win)
			return
		}
		s.startNodeAndDashboard()
	})
	unlock.Importance = widget.HighImportance
	pw.OnSubmitted = func(string) { unlock.OnTapped() }
	s.win.SetContent(container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle("⌾ Perihelion", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Wallet entsperren"),
		pw, unlock,
	)))
}

// --- main dashboard ---

func (s *appState) startNodeAndDashboard() {
	if s.node == nil {
		s.node = p2p.New(s.chain, func(string, ...any) {})
		s.node.SetPeerStore(filepath.Join(dataDir(), "peers.txt"))
		_ = s.node.Start("", readSeeds()) // outbound only: no inbound port, minimal attack surface
		s.setIntensity(loadConfig().Intensity)
	}
	s.showDashboard()
}

// setIntensity stops any running miner and restarts it at the requested
// level, persisting the choice.
func (s *appState) setIntensity(level string) {
	if s.mineCancel != nil {
		s.mineCancel()
		s.mineCancel = nil
	}
	s.intensity = level
	saveConfig(appConfig{Intensity: level})
	threads := threadsFor(level)
	if threads == 0 {
		return
	}
	core.SetMinerThreads(threads)
	ctx, cancel := context.WithCancel(context.Background())
	s.mineCancel = cancel
	s.cancel = cancel
	go core.MineLoop(ctx, s.chain, s.wallet.Address(), 0, core.MineOpts{
		OnBlock: s.node.BroadcastBlock,
		Ready:   s.node.Synced,
	})
}

func (s *appState) showDashboard() {
	addr := wallet.EncodeAddress(s.wallet.Address())

	spendable := widget.NewLabelWithStyle("… PER", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	immature := widget.NewLabel("")
	height := widget.NewLabel("…")
	supply := widget.NewLabel("…")
	peers := widget.NewLabel("…")
	mineStatus := widget.NewLabelWithStyle("⛏ Mining aktiv", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	addrEntry := widget.NewEntry()
	addrEntry.SetText(addr)
	addrEntry.OnChanged = func(string) { addrEntry.SetText(addr) } // read-only but selectable
	copyAddr := widget.NewButtonWithIcon("Adresse kopieren", theme.ContentCopyIcon(), func() {
		s.win.Clipboard().SetContent(addr)
	})

	intensitySel := widget.NewSelect(intensityLevels, nil)
	intensitySel.SetSelected(s.intensity)
	intensityHint := widget.NewLabel("")
	updateHint := func(level string) {
		if t := threadsFor(level); t == 0 {
			intensityHint.SetText("Mining pausiert")
		} else {
			intensityHint.SetText(fmt.Sprintf("%d von %d Kernen · ~%d MB Speicher", t, runtime.NumCPU(), t*64))
		}
	}
	updateHint(s.intensity)
	intensitySel.OnChanged = func(level string) {
		s.setIntensity(level)
		updateHint(level)
	}

	stats := container.NewGridWithColumns(2,
		widget.NewCard("Verfügbar", "", container.NewVBox(spendable, immature)),
		widget.NewCard("Status", "", container.NewVBox(mineStatus, peers)),
		widget.NewCard("Blockhöhe", "", height),
		widget.NewCard("Umlaufmenge", "", supply),
	)
	mineCard := widget.NewCard("Mining-Intensität", "",
		container.NewVBox(intensitySel, intensityHint))

	// send form
	toEntry := widget.NewEntry()
	toEntry.SetPlaceHolder("Empfänger-Adresse (per1…)")
	amtEntry := widget.NewEntry()
	amtEntry.SetPlaceHolder("Betrag in PER")
	sendBtn := widget.NewButtonWithIcon("Senden", theme.MailSendIcon(), func() {
		s.doSend(toEntry, amtEntry)
	})
	sendBtn.Importance = widget.HighImportance
	sendCard := widget.NewCard("Senden", "", container.NewVBox(toEntry, amtEntry, sendBtn))

	blocksList := widget.NewLabel("Lade Blöcke…")
	blocksCard := widget.NewCard("Letzte Blöcke", "", blocksList)

	phraseBtn := widget.NewButtonWithIcon("Wiederherstellungs-Phrase anzeigen", theme.VisibilityIcon(), s.showPhraseDialog)

	content := container.NewVScroll(container.NewVBox(
		widget.NewCard("", "", container.NewBorder(nil, nil, nil, copyAddr, addrEntry)),
		stats, mineCard, sendCard, blocksCard, phraseBtn,
	))
	s.win.SetContent(content)

	refresh := func() {
		stt, err := s.chain.Stats()
		if err != nil {
			return
		}
		sp, im, _ := s.chain.Balance(s.wallet.Address())
		fyne.Do(func() {
			spendable.SetText(core.FormatAmount(sp) + " PER")
			if im > 0 {
				immature.SetText("+ " + core.FormatAmount(im) + " PER reifen noch")
			} else {
				immature.SetText("")
			}
			height.SetText(fmt.Sprintf("%d", stt.Height))
			supply.SetText(core.FormatAmount(stt.Emitted-stt.Burned) + " PER")
			peers.SetText(fmt.Sprintf("%d Peers · %d Tx wartend", s.node.PeerCount(), stt.Mempool))
			if t := threadsFor(s.intensity); t == 0 {
				mineStatus.SetText("Mining pausiert")
			} else {
				mineStatus.SetText(fmt.Sprintf("⛏ Mining · %d Kerne", t))
			}
			blocksList.SetText(s.recentBlocksText())
		})
	}
	refresh()
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}

func (s *appState) recentBlocksText() string {
	stt, err := s.chain.Stats()
	if err != nil {
		return ""
	}
	var b strings.Builder
	mineAddr := s.wallet.Address()
	shown := 0
	for h := stt.Height; h > 0 && shown < 8; h-- {
		blk, err := s.chain.BlockByHeight(h)
		if err != nil {
			break
		}
		cb := blk.Txs[0]
		var reward uint64
		for i := range cb.Outputs {
			reward += cb.Outputs[i].Value
		}
		tag := ""
		if len(cb.Outputs) > 0 && cb.Outputs[0].Addr == mineAddr {
			tag = "  ⛏ von dir"
		}
		t := time.Unix(blk.Header.Time, 0).Format("15:04:05")
		fmt.Fprintf(&b, "#%d   %s   %s PER%s\n", h, t, core.FormatAmount(reward), tag)
		shown++
	}
	return b.String()
}

func (s *appState) doSend(toEntry, amtEntry *widget.Entry) {
	to, err := wallet.DecodeAddress(toEntry.Text)
	if err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	amount, err := core.ParseAmount(amtEntry.Text)
	if err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	fee, _ := core.ParseAmount("0.001")
	confirmMsg := fmt.Sprintf("%s PER senden an\n%s\n\nGebühr: 0.001 PER (die Hälfte wird verbrannt).", core.FormatAmount(amount), toEntry.Text)
	dialog.ShowConfirm("Senden bestätigen", confirmMsg, func(ok bool) {
		if !ok {
			return
		}
		tx, err := wallet.BuildSend(s.chain, s.wallet, to, amount, fee)
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		if err := s.chain.SubmitTx(tx); err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		s.node.BroadcastTx(tx)
		toEntry.SetText("")
		amtEntry.SetText("")
		dialog.ShowInformation("Gesendet", "Transaktion versendet — sie wird mit dem nächsten Block bestätigt.", s.win)
	}, s.win)
}

func (s *appState) showPhraseDialog() {
	pw := widget.NewPasswordEntry()
	pw.SetPlaceHolder("Wallet-Passwort")
	dialog.ShowForm("Phrase anzeigen", "Anzeigen", "Abbrechen",
		[]*widget.FormItem{widget.NewFormItem("Passwort", pw)},
		func(ok bool) {
			if !ok {
				return
			}
			m, err := s.wallet.RevealMnemonic(pw.Text)
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			e := widget.NewMultiLineEntry()
			e.SetText(m)
			e.SetMinRowsVisible(4)
			dialog.ShowCustom("Deine 24 Wörter", "Schließen", e, s.win)
		}, s.win)
}
