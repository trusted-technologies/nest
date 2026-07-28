// Package tui implements nest's interactive terminal interface: a loop of
// huh forms over the configuration file.
//
// The TUI only edits an in-memory copy of the configuration; nothing is
// written to disk until "Save & exit" is chosen. Long-running work (backup
// runs, restores, snapshot listings) is delegated to subprocesses of the
// nest binary itself so the TUI stays decoupled from the engine.
package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/trusted-technologies/nest/internal/config"
)

// app holds the TUI state: the in-memory configuration being edited.
type app struct {
	cfg        *config.Config
	configPath string
	dirty      bool
}

// Run starts the interactive TUI against the given config path.
// firstInstall enables the welcome wizard flow.
func Run(configPath string, firstInstall bool) error {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return errors.New("interactive TUI requires a terminal")
	}
	cfg, err := config.LoadOrEmpty(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	a := &app{cfg: cfg, configPath: configPath}
	if firstInstall {
		if err := a.wizard(); err != nil {
			return err
		}
	}
	return a.mainMenu()
}

// mainMenu is the root select loop; it returns when the user exits.
func (a *app) mainMenu() error {
	for {
		desc := "Config: " + a.configPath
		if a.dirty {
			desc += "  (unsaved changes)"
		}
		var choice string
		err := runFields(huh.NewSelect[string]().
			Title("nest — server backups").
			Description(desc).
			Options(
				huh.NewOption("Remotes — where backups are stored", "remotes"),
				huh.NewOption("Jobs — what is backed up", "jobs"),
				huh.NewOption("Run backup now", "run"),
				huh.NewOption("Snapshots", "snapshots"),
				huh.NewOption("Restore", "restore"),
				huh.NewOption("Notifications (Telegram)", "notifications"),
				huh.NewOption("Save & exit", "save"),
				huh.NewOption("Exit without saving", "quit"),
			).
			Value(&choice))
		switch {
		case aborted(err):
			if a.confirmQuit() {
				return nil
			}
			continue
		case err != nil:
			return err
		}

		switch choice {
		case "remotes":
			err = a.remotesMenu()
		case "jobs":
			err = a.jobsMenu()
		case "run":
			err = a.runBackup()
		case "snapshots":
			err = a.listSnapshots()
		case "restore":
			err = a.restore()
		case "notifications":
			err = a.telegramForm()
		case "save":
			if a.saveConfig() {
				return nil
			}
		case "quit":
			if a.confirmQuit() {
				return nil
			}
		}
		if err != nil && !aborted(err) {
			return err
		}
	}
}

// confirmQuit reports whether the user may leave; with unsaved changes it
// asks for confirmation first.
func (a *app) confirmQuit() bool {
	if !a.dirty {
		return true
	}
	ok, err := askConfirm("Discard unsaved changes?", "", false)
	return err == nil && ok
}

// telegramForm edits the Telegram notification settings.
func (a *app) telegramForm() error {
	tg := a.cfg.Notifications.Telegram
	err := runFields(
		huh.NewNote().
			Title("Telegram notifications").
			Description("nest messages you after backup runs.\nCreate a bot with @BotFather to get a token, then send it a message and read the chat ID via getUpdates.").
			Next(true),
		huh.NewConfirm().Title("Enabled").Value(&tg.Enabled),
		huh.NewInput().Title("Bot token").Placeholder("123456:ABC-DEF...").Password(true).Value(&tg.BotToken),
		huh.NewInput().Title("Chat ID").Placeholder("-1001234567890").Value(&tg.ChatID),
		huh.NewInput().Title("Thread ID (optional)").Description("For forum-style group topics.").Value(&tg.ThreadID),
	)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	a.cfg.Notifications.Telegram = tg
	a.dirty = true
	return nil
}

// saveConfig persists the configuration. Validation warnings are shown
// first (saving is still allowed) and the global encryption password is
// generated and displayed on first save. Returns false when saving failed.
func (a *app) saveConfig() bool {
	if warns := a.cfg.Validate(); len(warns) > 0 {
		showNote("Configuration warnings",
			strings.Join(warns, "\n")+"\n\nThe configuration will still be saved.")
	}
	a.cfg.Version = 1
	generated := ""
	if a.cfg.EncryptionPassword == "" {
		pw, err := genPassword(24)
		if err != nil {
			showNote("Error", "Could not generate an encryption password: "+err.Error())
			return false
		}
		a.cfg.EncryptionPassword = pw
		generated = pw
	}
	if err := config.Save(a.configPath, a.cfg); err != nil {
		showNote("Save failed", err.Error())
		return false
	}
	a.dirty = false
	if generated != "" {
		showNote("Your encryption password",
			generated+"\n\nStore it safely — without it your backups cannot be restored.\nIt is also kept in "+a.configPath+" (mode 0600).")
	}
	showNote("Saved", "Configuration written to "+a.configPath)
	return true
}
