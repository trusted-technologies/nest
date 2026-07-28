// Package engine executes backup, restore and maintenance operations
// against the configured remotes.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/notify"
	"github.com/trusted-technologies/nest/internal/remote"
	"github.com/trusted-technologies/nest/internal/schedule"
)

// Engine runs backup jobs described by Cfg.
type Engine struct {
	Cfg *config.Config
	Log *log.Logger

	// Verbose enables debug logging of subprocess output.
	Verbose bool

	host     string
	notifier *notify.Notifier
}

// New creates an Engine operating on cfg.
func New(cfg *config.Config) *Engine {
	host, _ := os.Hostname()
	return &Engine{
		Cfg:      cfg,
		Log:      log.New(os.Stdout, "", log.LstdFlags),
		host:     host,
		notifier: notify.New(cfg.Notifications.Telegram),
	}
}

// debugf logs at debug level when Verbose is set.
func (e *Engine) debugf(format string, args ...any) {
	if e.Verbose {
		e.Log.Printf("DEBUG "+format, args...)
	}
}

// RunBackup runs the selected backup jobs. A specific job is selected by
// name (error if unknown). With scheduled=true only enabled jobs whose
// schedule is due within window run (empty schedule = always due);
// otherwise all enabled jobs run. A failing job is logged and notified but
// does not abort the remaining jobs; an aggregated error is returned at
// the end if any job failed.
func (e *Engine) RunBackup(ctx context.Context, jobName string, scheduled bool, window time.Duration) error {
	jobs, err := e.selectJobs(jobName, scheduled, window)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		e.Log.Printf("no jobs due to run")
		return nil
	}
	var errs []error
	for i := range jobs {
		job := jobs[i]
		e.Log.Printf("[%s] starting backup (type %s, host %s)", job.Name, job.Type, e.host)
		e.notifier.Start(job.Name)
		start := time.Now()
		if err := e.runJob(ctx, &job); err != nil {
			e.Log.Printf("[%s] backup FAILED: %v", job.Name, err)
			e.notifier.Failure(job.Name, err)
			errs = append(errs, fmt.Errorf("job %s: %w", job.Name, err))
			continue
		}
		d := time.Since(start)
		e.Log.Printf("[%s] backup finished in %s", job.Name, d.Round(time.Second))
		e.notifier.Success(job.Name, d)
	}
	return errors.Join(errs...)
}

// selectJobs picks the jobs to run according to the RunBackup rules.
func (e *Engine) selectJobs(jobName string, scheduled bool, window time.Duration) ([]config.Job, error) {
	if jobName != "" {
		j := e.Cfg.FindJob(jobName)
		if j == nil {
			return nil, fmt.Errorf("job %q not found in config", jobName)
		}
		return []config.Job{*j}, nil
	}
	var out []config.Job
	now := time.Now()
	for _, j := range e.Cfg.EnabledJobs() {
		if scheduled {
			due, err := schedule.Due(j.Schedule, now, window)
			if err != nil {
				return nil, fmt.Errorf("job %q: %w", j.Name, err)
			}
			if !due {
				continue
			}
		}
		out = append(out, j)
	}
	return out, nil
}

// runJob dispatches a single job to its backend.
func (e *Engine) runJob(ctx context.Context, job *config.Job) error {
	r := e.Cfg.FindRemote(job.Remote)
	if r == nil {
		return fmt.Errorf("job %q: remote %q not found in config", job.Name, job.Remote)
	}
	if job.Incremental {
		return e.runResticJob(ctx, job, r)
	}
	return e.runArchiveJob(ctx, job, r)
}

// RunRestore restores snapshot of a restic job into target, optionally
// limited to includePath. Archive jobs cannot be restored automatically.
func (e *Engine) RunRestore(ctx context.Context, jobName, snapshot, target, includePath string) error {
	job, r, err := e.jobAndRemote(jobName)
	if err != nil {
		return err
	}
	if !job.Incremental {
		archiveTarget, _, terr := remote.ArchiveTarget(r, job.Name)
		if terr != nil {
			return terr
		}
		return fmt.Errorf("job %q uses plain tar.gz archives; download the archive from %s and untar it manually, e.g.:\n  tar -xzf <archive>.tar.gz -C %s", job.Name, archiveTarget, target)
	}
	bin, env, base, err := e.resticSetup(job, r)
	if err != nil {
		return err
	}
	if snapshot == "" {
		snapshot = "latest"
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("create restore target %s: %w", target, err)
	}
	args := append(base, "restore", snapshot, "--target", target)
	if includePath != "" {
		args = append(args, "--include", includePath)
	}
	e.Log.Printf("[%s] restoring snapshot %s to %s", job.Name, snapshot, target)
	if _, err := e.runCmd(ctx, bin, args, env); err != nil {
		return err
	}
	e.Log.Printf("[%s] restore to %s complete", job.Name, target)
	return nil
}

// ListJobs prints a table of all configured jobs.
func (e *Engine) ListJobs() {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tREMOTE\tBACKEND\tSCHEDULE\tENABLED")
	for _, j := range e.Cfg.Jobs {
		backend := "archive"
		if j.Incremental {
			backend = "restic"
		}
		sched := j.Schedule
		if sched == "" {
			sched = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%t\n", j.Name, j.Type, j.Remote, backend, sched, j.Enabled)
	}
	_ = w.Flush()
}

// ListRemotes prints a table of all configured remotes.
func (e *Engine) ListRemotes() {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tTARGET")
	for _, r := range e.Cfg.Remotes {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Type, remoteSummary(&r))
	}
	_ = w.Flush()
}

// remoteSummary returns a short human-readable destination description.
func remoteSummary(r *config.Remote) string {
	switch r.Type {
	case config.RemoteS3:
		return "bucket " + r.Bucket
	case config.RemoteSFTP:
		return fmt.Sprintf("%s@%s", r.User, r.Host)
	case config.RemoteRclone:
		return r.RcloneRemote
	}
	return ""
}

// ListSnapshots prints the restic snapshot list of a job.
func (e *Engine) ListSnapshots(ctx context.Context, jobName string) error {
	job, r, err := e.resticJobAndRemote(jobName)
	if err != nil {
		return err
	}
	bin, env, base, err := e.resticSetup(job, r)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, append(base, "snapshots")...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restic snapshots: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Repair runs `restic rebuild-index` for a job's repository; with full it
// additionally runs `restic repair index` and `restic repair snapshots`.
func (e *Engine) Repair(ctx context.Context, jobName string, full bool) error {
	job, r, err := e.resticJobAndRemote(jobName)
	if err != nil {
		return err
	}
	bin, env, base, err := e.resticSetup(job, r)
	if err != nil {
		return err
	}
	e.Log.Printf("[%s] running restic rebuild-index", job.Name)
	if _, err := e.runCmd(ctx, bin, append(base, "rebuild-index"), env); err != nil {
		return err
	}
	if full {
		e.Log.Printf("[%s] running restic repair index", job.Name)
		if _, err := e.runCmd(ctx, bin, append(base, "repair", "index"), env); err != nil {
			return err
		}
		e.Log.Printf("[%s] running restic repair snapshots", job.Name)
		if _, err := e.runCmd(ctx, bin, append(base, "repair", "snapshots"), env); err != nil {
			return err
		}
	}
	e.Log.Printf("[%s] repository repair finished", job.Name)
	return nil
}

// jobAndRemote resolves a job and its remote by name.
func (e *Engine) jobAndRemote(jobName string) (*config.Job, *config.Remote, error) {
	job := e.Cfg.FindJob(jobName)
	if job == nil {
		return nil, nil, fmt.Errorf("job %q not found in config", jobName)
	}
	r := e.Cfg.FindRemote(job.Remote)
	if r == nil {
		return nil, nil, fmt.Errorf("job %q: remote %q not found in config", job.Name, job.Remote)
	}
	return job, r, nil
}

// resticJobAndRemote is jobAndRemote plus a check that the job is
// restic-based (incremental).
func (e *Engine) resticJobAndRemote(jobName string) (*config.Job, *config.Remote, error) {
	job, r, err := e.jobAndRemote(jobName)
	if err != nil {
		return nil, nil, err
	}
	if !job.Incremental {
		return nil, nil, fmt.Errorf("job %q uses plain archives, not restic; this command only works on incremental jobs", jobName)
	}
	return job, r, nil
}

// runCmd runs name with args, returning its stdout. stderr is captured and
// embedded in the error on failure; stdout is logged at debug level.
func (e *Engine) runCmd(ctx context.Context, name string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	e.debugf("exec: %s %s", name, strings.Join(args, " "))
	err := cmd.Run()
	if s := strings.TrimSpace(stdout.String()); s != "" {
		e.debugf("%s stdout:\n%s", name, s)
	}
	if err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// runPipeline wires producer's stdout into consumer's stdin and runs both,
// checking both exit codes. The consumer's stdout is logged at debug level.
func (e *Engine) runPipeline(producer, consumer *exec.Cmd) error {
	var prodErr, consErr, consOut bytes.Buffer
	producer.Stderr = &prodErr
	consumer.Stderr = &consErr
	consumer.Stdout = &consOut
	pipe, err := producer.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe %s: %w", producer.Args[0], err)
	}
	consumer.Stdin = pipe
	e.debugf("pipeline: %s | %s", strings.Join(producer.Args, " "), strings.Join(consumer.Args, " "))
	if err := producer.Start(); err != nil {
		return fmt.Errorf("start %s: %w", producer.Args[0], err)
	}
	if err := consumer.Start(); err != nil {
		return fmt.Errorf("start %s: %w", consumer.Args[0], err)
	}
	perr := producer.Wait()
	cerr := consumer.Wait()
	if s := strings.TrimSpace(consOut.String()); s != "" {
		e.debugf("%s stdout:\n%s", consumer.Args[0], s)
	}
	if perr != nil {
		return fmt.Errorf("%s: %w: %s", producer.Args[0], perr, strings.TrimSpace(prodErr.String()))
	}
	if cerr != nil {
		return fmt.Errorf("%s: %w: %s", consumer.Args[0], cerr, strings.TrimSpace(consErr.String()))
	}
	return nil
}

// exitCode extracts the process exit code from err, or -1.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// envList appends a map of environment variables to base as KEY=value.
func envList(base []string, m map[string]string) []string {
	for k, v := range m {
		base = append(base, k+"="+v)
	}
	return base
}
