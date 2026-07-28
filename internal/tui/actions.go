package tui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// runBackup asks which job to back up and runs `nest backup` as a
// subprocess with inherited stdio.
func (a *app) runBackup() error {
	if len(a.cfg.Jobs) == 0 {
		showNote("No jobs", "Add a job first.")
		return nil
	}
	opts := make([]huh.Option[int], 0, len(a.cfg.Jobs)+1)
	opts = append(opts, huh.NewOption("All jobs", -1))
	for i := range a.cfg.Jobs {
		opts = append(opts, huh.NewOption(jobLabel(&a.cfg.Jobs[i]), i))
	}
	sel := -1
	err := runFields(huh.NewSelect[int]().
		Title("Run backup now").
		Options(opts...).
		Value(&sel))
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}

	args := []string{"backup"}
	what := "Backup"
	if sel >= 0 {
		args = append(args, "--job", a.cfg.Jobs[sel].Name)
		what = fmt.Sprintf("Backup of %q", a.cfg.Jobs[sel].Name)
	}
	reportResult(what, runNest(args...))
	return nil
}

// listSnapshots runs `nest snapshots --job X` as a subprocess with
// inherited stdio.
func (a *app) listSnapshots() error {
	idx, err := a.pickJob("Show snapshots of which job?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		return nil
	}
	reportResult("Snapshots", runNest("snapshots", "--job", a.cfg.Jobs[idx].Name))
	return nil
}

// restore asks for snapshot ID and target directory, confirms, then runs
// `nest restore` as a subprocess with inherited stdio.
func (a *app) restore() error {
	idx, err := a.pickJob("Restore which job?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		return nil
	}
	job := a.cfg.Jobs[idx].Name

	snapshot := "latest"
	target := "/var/lib/nest/restore"
	err = runFields(
		huh.NewInput().Title("Snapshot ID").Value(&snapshot).Validate(validateRequired),
		huh.NewInput().Title("Restore to directory").Value(&target).Validate(validateRequired),
	)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}

	ok, err := askConfirm("Start the restore?",
		fmt.Sprintf("This runs:\n\n  nest restore --job %s --snapshot %s --target %s\n\nExisting files in the target may be overwritten.",
			job, snapshot, target), true)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	reportResult("Restore", runNest("restore", "--job", job, "--snapshot", snapshot, "--target", target))
	return nil
}
