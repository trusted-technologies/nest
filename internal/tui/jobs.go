package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/jobs"
)

// jobsMenu is the Jobs submenu loop.
func (a *app) jobsMenu() error {
	for {
		var action string
		err := runFields(
			huh.NewNote().Title("Jobs").Description(a.jobList()),
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Add a job", "add"),
					huh.NewOption("Edit a job", "edit"),
					huh.NewOption("Delete a job", "delete"),
					huh.NewOption("Toggle enabled", "toggle"),
					huh.NewOption("Back", "back"),
				).
				Value(&action))
		if aborted(err) {
			return nil
		}
		if err != nil {
			return err
		}

		switch action {
		case "add":
			err = a.addJob()
		case "edit":
			err = a.editJob()
		case "delete":
			err = a.deleteJob()
		case "toggle":
			err = a.toggleJob()
		case "back":
			return nil
		}
		if err != nil && !aborted(err) {
			return err
		}
	}
}

// jobList renders the jobs overview shown above the submenu.
func (a *app) jobList() string {
	if len(a.cfg.Jobs) == 0 {
		return "No backup jobs configured yet."
	}
	var sb strings.Builder
	for i := range a.cfg.Jobs {
		fmt.Fprintf(&sb, "• %s\n", jobLabel(&a.cfg.Jobs[i]))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// jobLabel is the one-line summary of a job.
func jobLabel(j *config.Job) string {
	backend := "archive (.tar.gz)"
	if j.Incremental {
		backend = "restic"
	}
	sched := j.Schedule
	if sched == "" {
		sched = "every tick"
	}
	state := "enabled"
	if !j.Enabled {
		state = "disabled"
	}
	return fmt.Sprintf("%s [%s] → %s (%s, %s, %s)", j.Name, j.Type, j.Remote, backend, sched, state)
}

// pickJob asks the user to choose a job and returns its index
// (-1 when there are none).
func (a *app) pickJob(title string) (int, error) {
	if len(a.cfg.Jobs) == 0 {
		return -1, nil
	}
	opts := make([]huh.Option[int], 0, len(a.cfg.Jobs))
	for i := range a.cfg.Jobs {
		opts = append(opts, huh.NewOption(jobLabel(&a.cfg.Jobs[i]), i))
	}
	idx := 0
	if err := runFields(huh.NewSelect[int]().Title(title).Options(opts...).Value(&idx)); err != nil {
		return -1, err
	}
	return idx, nil
}

// uniqueJobName validates that a job name is non-empty and unique,
// excluding origName (the name before an edit).
func (a *app) uniqueJobName(origName string) func(string) error {
	return func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return fmt.Errorf("name is required")
		}
		if s != origName && a.cfg.FindJob(s) != nil {
			return fmt.Errorf("a job named %q already exists", s)
		}
		return nil
	}
}

// addJob offers the built-in presets and runs the job form prefilled from
// the chosen preset.
func (a *app) addJob() error {
	if len(a.cfg.Remotes) == 0 {
		showNote("No remotes", "Add a remote first — jobs back up to a remote.")
		return nil
	}
	presets := jobs.Presets()
	opts := make([]huh.Option[int], 0, len(presets))
	for i, p := range presets {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s — %s", p.Title, p.Description), i))
	}
	preset := 0
	err := runFields(huh.NewSelect[int]().
		Title("What do you want to back up?").
		Options(opts...).
		Value(&preset))
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	j := presets[preset].Job
	if err := a.jobForm(&j, ""); err != nil {
		if aborted(err) {
			return nil
		}
		return err
	}
	a.cfg.Jobs = append(a.cfg.Jobs, j)
	a.dirty = true
	return nil
}

// editJob edits a copy of the chosen job and writes it back on success.
func (a *app) editJob() error {
	if len(a.cfg.Remotes) == 0 {
		showNote("No remotes", "Add a remote first — jobs back up to a remote.")
		return nil
	}
	idx, err := a.pickJob("Edit which job?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No jobs", "Add a job first.")
		return nil
	}
	j := a.cfg.Jobs[idx]
	if err := a.jobForm(&j, j.Name); err != nil {
		if aborted(err) {
			return nil
		}
		return err
	}
	a.cfg.Jobs[idx] = j
	a.dirty = true
	return nil
}

// deleteJob removes a job after confirmation.
func (a *app) deleteJob() error {
	idx, err := a.pickJob("Delete which job?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No jobs", "Nothing to delete.")
		return nil
	}
	ok, err := askConfirm(fmt.Sprintf("Delete job %q?", a.cfg.Jobs[idx].Name),
		"Snapshots already stored on the remote are kept.", false)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	a.cfg.Jobs = slices.Delete(a.cfg.Jobs, idx, idx+1)
	a.dirty = true
	return nil
}

// toggleJob flips the Enabled flag of the chosen job.
func (a *app) toggleJob() error {
	idx, err := a.pickJob("Toggle which job?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No jobs", "Add a job first.")
		return nil
	}
	a.cfg.Jobs[idx].Enabled = !a.cfg.Jobs[idx].Enabled
	a.dirty = true
	return nil
}

// jobForm runs the job editor for j (prefilled, e.g. from a preset).
// origName is excluded from the name-uniqueness check ("" when adding).
// The caller must guarantee at least one remote exists.
func (a *app) jobForm(j *config.Job, origName string) error {
	remoteOpts := make([]huh.Option[string], 0, len(a.cfg.Remotes))
	for i := range a.cfg.Remotes {
		r := &a.cfg.Remotes[i]
		remoteOpts = append(remoteOpts, huh.NewOption(remoteLabel(r), r.Name))
	}
	// Preselect the first remote when the prefilled one no longer exists.
	if a.cfg.FindRemote(j.Remote) == nil {
		j.Remote = a.cfg.Remotes[0].Name
	}

	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewInput().Title("Name").Value(&j.Name).Validate(a.uniqueJobName(origName)),
			huh.NewSelect[string]().
				Title("Remote").
				Description("Where this job's backups are stored.").
				Options(remoteOpts...).
				Value(&j.Remote),
			huh.NewConfirm().
				Title("Incremental (restic) — off = plain .tar.gz archives").
				Value(&j.Incremental),
			huh.NewInput().
				Title("Schedule").
				Placeholder("04:00 or 30 3 * * 0").
				Description("Daily at HH:MM, a 5-field cron expression, or empty = every timer tick.").
				Value(&j.Schedule).
				Validate(validateSchedule),
			huh.NewConfirm().Title("Enabled").Value(&j.Enabled),
		),
	}

	excludes := strings.Join(j.Exclude, "\n")
	databases := strings.Join(j.Databases, ", ")
	dbPort := ""
	switch j.Type {
	case config.JobFolder:
		groups = append(groups, huh.NewGroup(
			huh.NewInput().
				Title("Source folder").
				Placeholder("/var/www").
				Value(&j.Source).
				Validate(validateRequired),
			huh.NewText().
				Title("Exclude patterns (optional)").
				Description("One pattern per line.").
				Lines(5).
				Value(&excludes),
		))
	case config.JobMySQL, config.JobPostgreSQL:
		dbPort = strconv.Itoa(j.DBPort)
		if j.DBPort == 0 {
			dbPort = "3306"
			if j.Type == config.JobPostgreSQL {
				dbPort = "5432"
			}
		}
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title("DB host").Value(&j.DBHost).Validate(validateRequired),
			huh.NewInput().Title("DB port").Value(&dbPort).Validate(validatePort),
			huh.NewInput().Title("DB user").Value(&j.DBUser),
			huh.NewInput().Title("DB password").Password(true).Value(&j.DBPassword),
			huh.NewInput().
				Title("Databases (comma-separated, empty = all)").
				Placeholder("db1, db2").
				Value(&databases),
		))
	}

	last := itoaOrEmpty(j.Retention.Last)
	daily := itoaOrEmpty(j.Retention.Daily)
	weekly := itoaOrEmpty(j.Retention.Weekly)
	monthly := itoaOrEmpty(j.Retention.Monthly)
	groups = append(groups, huh.NewGroup(
		huh.NewNote().
			Title("Retention").
			Description("How many snapshots to keep; 0 or empty = unlimited."),
		huh.NewInput().Title("Keep last").Value(&last).Validate(validateOptInt),
		huh.NewInput().Title("Keep daily").Value(&daily).Validate(validateOptInt),
		huh.NewInput().Title("Keep weekly").Value(&weekly).Validate(validateOptInt),
		huh.NewInput().Title("Keep monthly").Value(&monthly).Validate(validateOptInt),
		huh.NewInput().
			Title("Restic password (optional)").
			Description("Overrides the global encryption_password for this job.").
			Password(true).
			Value(&j.ResticPassword),
	))

	if err := runGroups(groups...); err != nil {
		return err
	}

	j.Name = strings.TrimSpace(j.Name)
	j.Schedule = strings.TrimSpace(j.Schedule)
	j.Exclude = splitLines(excludes)
	j.Retention = config.Retention{
		Last:    parseOptInt(last),
		Daily:   parseOptInt(daily),
		Weekly:  parseOptInt(weekly),
		Monthly: parseOptInt(monthly),
	}
	if j.Type == config.JobMySQL || j.Type == config.JobPostgreSQL {
		j.DBPort, _ = strconv.Atoi(strings.TrimSpace(dbPort))
		j.Databases = splitComma(databases)
	}
	return nil
}

// itoaOrEmpty renders 0 as an empty string for optional integer inputs.
func itoaOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
