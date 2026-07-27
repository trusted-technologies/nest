package tui

import (
	"os/exec"

	"github.com/charmbracelet/huh"
)

// wizard is the first-install flow shown before the main menu: welcome,
// first remote, first job, Telegram, save, then an optional test backup
// and systemd timer activation. Aborted (Esc) steps are skipped.
func (a *app) wizard() error {
	err := runFields(huh.NewNote().
		Title("Welcome to nest").
		Description("nest backs up this server to S3, SFTP or any rclone remote — on a schedule, with deduplication and encryption.\n\nYour configuration lives at:\n  " + a.configPath + "\n\nThis wizard creates your first remote and backup job.").
		Next(true).
		NextLabel("Let's go"))
	if err != nil && !aborted(err) {
		return err
	}

	if err := a.addRemote(); err != nil {
		return err
	}
	if err := a.addJob(); err != nil {
		return err
	}
	if err := a.telegramForm(); err != nil {
		return err
	}

	if !a.saveConfig() {
		// The error was already shown; drop into the main menu so the
		// user can fix the problem and save again.
		return nil
	}

	ok, err := askConfirm("Run a test backup now?", "This runs `nest backup` for all jobs.", true)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	backupErr := runNest("backup")
	reportResult("Test backup", backupErr)
	if backupErr != nil {
		return nil
	}

	showNote("Enable the backup timer",
		"Backups run via a systemd timer. Enable it with:\n\n  systemctl enable --now nest.timer")
	if err := exec.Command("systemctl", "enable", "--now", "nest.timer").Run(); err != nil {
		showNote("Timer not enabled",
			"Could not run `systemctl enable --now nest.timer`:\n\n"+err.Error()+
				"\n\nEnable it manually once the nest service is installed.")
	}
	return nil
}
