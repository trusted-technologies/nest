package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/deps"
	"github.com/trusted-technologies/nest/internal/remote"
)

// defaultArchiveKeep is the number of archives kept when a job sets no
// explicit keep-last retention.
const defaultArchiveKeep = 7

// archiveName returns the UTC timestamped file name for a new archive.
// Timestamps sort lexicographically, which the retention logic relies on.
func archiveName(t time.Time) string {
	return t.UTC().Format("20060102-150405") + ".tar.gz"
}

// retentionDelete returns the names to remove so that the newest keep
// remain. names must be sorted oldest-first.
func retentionDelete(names []string, keep int) []string {
	if keep < 1 {
		keep = 1
	}
	if len(names) <= keep {
		return nil
	}
	return names[:len(names)-keep]
}

// runArchiveJob runs one plain tar.gz archive job, uploading through rclone.
func (e *Engine) runArchiveJob(ctx context.Context, job *config.Job, r *config.Remote) error {
	bin, err := deps.Ensure("rclone")
	if err != nil {
		return err
	}
	target, renv, err := remote.ArchiveTarget(r, job.Name)
	if err != nil {
		return err
	}
	env := envList(os.Environ(), renv)
	dest := strings.TrimSuffix(target, "/") + "/" + archiveName(time.Now())

	switch job.Type {
	case config.JobFolder:
		err = e.archiveFolder(ctx, bin, env, job, dest)
	case config.JobMySQL, config.JobPostgreSQL:
		err = e.archiveDB(ctx, bin, env, job, dest)
	default:
		err = fmt.Errorf("job %q: unsupported type %q", job.Name, job.Type)
	}
	if err != nil {
		return err
	}
	return e.archiveRetention(ctx, bin, env, target, job.Retention)
}

// archiveFolder streams a gzipped tar of the job's source into rclone rcat.
func (e *Engine) archiveFolder(ctx context.Context, bin string, env []string, job *config.Job, dest string) error {
	src := filepath.Clean(job.Source)
	tarArgs := []string{"-czf", "-", "-C", filepath.Dir(src)}
	for _, ex := range job.Exclude {
		tarArgs = append(tarArgs, "--exclude", ex)
	}
	tarArgs = append(tarArgs, filepath.Base(src))
	tar := exec.CommandContext(ctx, "tar", tarArgs...)
	rcat := exec.CommandContext(ctx, bin, "rcat", dest)
	rcat.Env = env
	e.Log.Printf("[%s] archiving %s to %s", job.Name, job.Source, dest)
	return e.runPipeline(tar, rcat)
}

// archiveDB dumps each database into a temporary directory and streams a
// gzipped tar of that directory into rclone rcat.
func (e *Engine) archiveDB(ctx context.Context, bin string, env []string, job *config.Job, dest string) error {
	dbs, err := e.listDatabases(ctx, job)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		return fmt.Errorf("job %q: no databases to back up", job.Name)
	}
	tmp, err := os.MkdirTemp("", "nest-archive-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	for _, db := range dbs {
		e.Log.Printf("[%s] dumping database %s", job.Name, db)
		dump, err := e.dumpCommand(ctx, job, db)
		if err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(tmp, db+".sql"))
		if err != nil {
			return fmt.Errorf("create dump file for %s: %w", db, err)
		}
		var stderr bytes.Buffer
		dump.Stdout = f
		dump.Stderr = &stderr
		runErr := dump.Run()
		closeErr := f.Close()
		if runErr != nil {
			return fmt.Errorf("dump database %s: %w: %s", db, runErr, strings.TrimSpace(stderr.String()))
		}
		if closeErr != nil {
			return fmt.Errorf("write dump of %s: %w", db, closeErr)
		}
	}
	tar := exec.CommandContext(ctx, "tar", "-czf", "-", "-C", tmp, ".")
	rcat := exec.CommandContext(ctx, bin, "rcat", dest)
	rcat.Env = env
	e.Log.Printf("[%s] archiving %d database dump(s) to %s", job.Name, len(dbs), dest)
	return e.runPipeline(tar, rcat)
}

// archiveRetention enforces keep-last on the uploaded archives. Daily,
// weekly and monthly policies are not supported for archive jobs.
func (e *Engine) archiveRetention(ctx context.Context, bin string, env []string, target string, rt config.Retention) error {
	keep := rt.Last
	if rt.Daily > 0 || rt.Weekly > 0 || rt.Monthly > 0 {
		e.Log.Printf("warning: daily/weekly/monthly retention is not supported for archive jobs; using keep-last")
	}
	if keep == 0 {
		keep = defaultArchiveKeep
	}
	out, err := e.runCmd(ctx, bin, []string{"lsf", strings.TrimSuffix(target, "/") + "/", "--files-only"}, env, nil)
	if err != nil {
		return fmt.Errorf("list archives: %w", err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if n := strings.TrimSpace(line); n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, old := range retentionDelete(names, keep) {
		e.Log.Printf("deleting old archive %s", old)
		if _, err := e.runCmd(ctx, bin, []string{"deletefile", strings.TrimSuffix(target, "/") + "/" + old}, env, nil); err != nil {
			return fmt.Errorf("delete archive %s: %w", old, err)
		}
	}
	return nil
}
