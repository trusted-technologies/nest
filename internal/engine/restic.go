package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/deps"
	"github.com/trusted-technologies/nest/internal/remote"
)

// systemDBs are databases never backed up by "all databases" jobs.
var systemDBs = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
	"template0":          true,
	"template1":          true,
	"postgres":           true,
}

// isSystemDB reports whether name is a database-internal schema that should
// be skipped when backing up all databases.
func isSystemDB(name string) bool {
	return systemDBs[name]
}

// resticSetup resolves the restic binary, environment and base arguments
// (sftp options) for a job. It also ensures the SSH keypair exists for sftp
// remotes.
func (e *Engine) resticSetup(job *config.Job, r *config.Remote) (bin string, env, base []string, err error) {
	bin, err = deps.Ensure("restic")
	if err != nil {
		return "", nil, nil, err
	}
	sshDir := filepath.Join(config.Dir, "ssh")
	if err := e.prepareSFTP(r, sshDir); err != nil {
		return "", nil, nil, err
	}
	env, err = e.resticEnv(job, r)
	if err != nil {
		return "", nil, nil, err
	}
	base = remote.ResticOptions(r, sshDir)
	return bin, env, base, nil
}

// prepareSFTP ensures the nest SSH keypair exists for sftp remotes and logs
// the public key when it was just generated.
func (e *Engine) prepareSFTP(r *config.Remote, sshDir string) error {
	if r.Type != config.RemoteSFTP {
		return nil
	}
	_, statErr := os.Stat(remote.DefaultKeyPath(sshDir))
	if _, _, err := remote.EnsureKeyPair(sshDir); err != nil {
		return fmt.Errorf("ensure ssh keypair: %w", err)
	}
	if statErr != nil {
		e.Log.Printf("generated a new SSH keypair; authorize this public key on the backup server (%s@%s):\n%s",
			r.User, r.Host, strings.TrimSpace(remote.PublicKey(sshDir)))
	}
	return nil
}

// resticPassword resolves the repository password: job override, then the
// global encryption password, then $NEST_RESTIC_PASSWORD.
func (e *Engine) resticPassword(job *config.Job) (string, error) {
	if job.ResticPassword != "" {
		return job.ResticPassword, nil
	}
	if e.Cfg.EncryptionPassword != "" {
		return e.Cfg.EncryptionPassword, nil
	}
	if pw := os.Getenv("NEST_RESTIC_PASSWORD"); pw != "" {
		return pw, nil
	}
	return "", fmt.Errorf("job %q: no restic password (set restic_password, encryption_password or NEST_RESTIC_PASSWORD)", job.Name)
}

// resticEnv builds the full environment for restic invocations of a job.
func (e *Engine) resticEnv(job *config.Job, r *config.Remote) ([]string, error) {
	repo, err := remote.ResticRepository(r, job.Name)
	if err != nil {
		return nil, err
	}
	pw, err := e.resticPassword(job)
	if err != nil {
		return nil, err
	}
	renv, err := remote.ResticEnv(r)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.CacheDir, 0o700); err != nil {
		e.debugf("cannot create cache dir %s: %v", config.CacheDir, err)
	}
	env := append(os.Environ(),
		"RESTIC_PASSWORD="+pw,
		"RESTIC_REPOSITORY="+repo,
		"XDG_CACHE_HOME="+config.CacheDir,
	)
	return envList(env, renv), nil
}

// runResticJob runs one incremental (restic) backup job.
func (e *Engine) runResticJob(ctx context.Context, job *config.Job, r *config.Remote) error {
	bin, env, base, err := e.resticSetup(job, r)
	if err != nil {
		return err
	}
	if err := e.resticEnsureRepo(ctx, bin, env, base); err != nil {
		return err
	}
	switch job.Type {
	case config.JobFolder:
		err = e.resticBackupFolder(ctx, bin, env, base, job)
	case config.JobMySQL, config.JobPostgreSQL:
		err = e.resticBackupDB(ctx, bin, env, base, job)
	default:
		err = fmt.Errorf("job %q: unsupported type %q", job.Name, job.Type)
	}
	if err != nil {
		return err
	}
	return e.resticForget(ctx, bin, env, base, job)
}

// resticEnsureRepo initializes the repository if it does not exist yet.
func (e *Engine) resticEnsureRepo(ctx context.Context, bin string, env, base []string) error {
	_, err := e.runCmd(ctx, bin, append(base, "snapshots"), env, nil)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "config file") {
		return fmt.Errorf("check repository: %w", err)
	}
	e.Log.Printf("restic repository does not exist yet; running restic init")
	if _, err := e.runCmd(ctx, bin, append(base, "init"), env, nil); err != nil {
		return fmt.Errorf("init repository: %w", err)
	}
	return nil
}

// resticBackupFolder backs up a folder source. Restic exit code 3 (some
// source files could not be read) is treated as success-with-warnings.
func (e *Engine) resticBackupFolder(ctx context.Context, bin string, env, base []string, job *config.Job) error {
	args := append(base, "backup", job.Source, "--tag", job.Name)
	for _, ex := range job.Exclude {
		args = append(args, "--exclude", ex)
	}
	_, err := e.runCmd(ctx, bin, args, env, nil)
	if err != nil && exitCode(err) == 3 {
		e.Log.Printf("[%s] warning: some files could not be read (restic exit code 3); snapshot created anyway", job.Name)
		return nil
	}
	return err
}

// resticBackupDB dumps each selected database and streams it into restic
// via `restic backup --stdin`.
func (e *Engine) resticBackupDB(ctx context.Context, bin string, env, base []string, job *config.Job) error {
	dbs, err := e.listDatabases(ctx, job)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		return fmt.Errorf("job %q: no databases to back up", job.Name)
	}
	for _, db := range dbs {
		e.Log.Printf("[%s] dumping database %s", job.Name, db)
		dump, err := e.dumpCommand(ctx, job, db)
		if err != nil {
			return err
		}
		restic := exec.CommandContext(ctx, bin,
			append(base, "backup", "--stdin", "--stdin-filename", db+".sql", "--tag", job.Name)...)
		restic.Env = env
		if err := e.runPipeline(dump, restic); err != nil {
			return fmt.Errorf("back up database %s: %w", db, err)
		}
	}
	return nil
}

// resticForget applies the job's retention policy. It is a no-op when all
// retention fields are zero.
func (e *Engine) resticForget(ctx context.Context, bin string, env, base []string, job *config.Job) error {
	rt := job.Retention
	if rt.Last == 0 && rt.Daily == 0 && rt.Weekly == 0 && rt.Monthly == 0 {
		return nil
	}
	args := append(base, "forget", "--tag", job.Name, "--prune")
	if rt.Last > 0 {
		args = append(args, "--keep-last", strconv.Itoa(rt.Last))
	}
	if rt.Daily > 0 {
		args = append(args, "--keep-daily", strconv.Itoa(rt.Daily))
	}
	if rt.Weekly > 0 {
		args = append(args, "--keep-weekly", strconv.Itoa(rt.Weekly))
	}
	if rt.Monthly > 0 {
		args = append(args, "--keep-monthly", strconv.Itoa(rt.Monthly))
	}
	e.Log.Printf("[%s] applying retention policy", job.Name)
	_, err := e.runCmd(ctx, bin, args, env, nil)
	return err
}

// dbDefaults returns host, port and user for a database job, applying the
// per-engine defaults.
func dbDefaults(job *config.Job) (host string, port int, user string) {
	host = job.DBHost
	if host == "" {
		host = "localhost"
	}
	port = job.DBPort
	user = job.DBUser
	if job.Type == config.JobMySQL {
		if port == 0 {
			port = 3306
		}
		if user == "" {
			user = "root"
		}
	} else {
		if port == 0 {
			port = 5432
		}
		if user == "" {
			user = "postgres"
		}
	}
	return host, port, user
}

// mysqlArgs builds the common connection arguments for mysql/mysqldump.
// For a local server (host "localhost") no -h/-P is passed at all: the
// client then uses the default unix socket — the same as `sudo mysql` —
// which makes passwordless socket auth (auth_socket/unix_socket) work.
// "127.0.0.1" or any other host goes over TCP as before.
//
// The password is NOT placed into argv (`-p<pw>`): it would be visible in
// /proc/<pid>/cmdline to other local users and would leak into nest's own
// error messages (which embed the full argv) and from there into logs and
// Telegram notifications. It travels via the MYSQL_PWD environment
// variable instead — /proc/<pid>/environ of a root process is not readable
// by other users. (MySQL itself warns that MYSQL_PWD is insecure only
// because of ancient shared-system argv sniffing; env is the lesser evil
// and the same channel PostgreSQL's PGPASSWORD uses.)
func mysqlArgs(job *config.Job) (args []string, env map[string]string) {
	host, port, user := dbDefaults(job)
	if host == "localhost" {
		args = []string{"-u", user}
	} else {
		args = []string{"-h", host, "-P", strconv.Itoa(port), "-u", user}
	}
	if job.DBPassword != "" {
		env = map[string]string{"MYSQL_PWD": job.DBPassword}
	}
	return args, env
}

// pgArgs builds the common connection arguments and environment for
// psql/pg_dump.
func pgArgs(job *config.Job) (args []string, env map[string]string) {
	host, port, user := dbDefaults(job)
	args = []string{"-h", host, "-p", strconv.Itoa(port), "-U", user}
	if job.DBPassword != "" {
		env = map[string]string{"PGPASSWORD": job.DBPassword}
	}
	return args, env
}

// listDatabases returns the databases of a job: the explicit list when
// configured, otherwise all non-system databases discovered on the server.
// Names are validated — a hostile database name (possible when an
// unprivileged user holds CREATEDB on the server) must never become a
// command-line option or a filesystem path further down the pipeline.
func (e *Engine) listDatabases(ctx context.Context, job *config.Job) ([]string, error) {
	var dbs []string
	if len(job.Databases) > 0 {
		dbs = job.Databases
	} else {
		var out string
		var err error
		switch job.Type {
		case config.JobMySQL:
			args, menv := mysqlArgs(job)
			args = append(args, "-N", "-B", "-e", "SHOW DATABASES;")
			out, err = e.runCmd(ctx, "mysql", args, envList(os.Environ(), menv), nil)
		case config.JobPostgreSQL:
			args, penv := pgArgs(job)
			args = append(args, "-t", "-A", "-c", "SELECT datname FROM pg_database WHERE datistemplate = false;", "postgres")
			out, err = e.runCmd(ctx, "psql", args, envList(os.Environ(), penv), nil)
		default:
			return nil, fmt.Errorf("job %q: unsupported database type %q", job.Name, job.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("job %q: list databases: %w", job.Name, err)
		}
		for _, line := range strings.Split(out, "\n") {
			name := strings.TrimSpace(line)
			if name == "" || isSystemDB(name) {
				continue
			}
			dbs = append(dbs, name)
		}
	}
	for _, db := range dbs {
		if err := validDBName(db); err != nil {
			return nil, fmt.Errorf("job %q: refusing to back up database with unsafe name %q: %w", job.Name, db, err)
		}
	}
	return dbs, nil
}

// validDBName rejects database names that could smuggle command-line
// options (leading '-'), filesystem paths ('/', '\') or control characters
// (including '\n', which would break line-based parsing) into dump
// commands, archive file names and restic's --stdin-filename.
func validDBName(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	if name[0] == '-' {
		return fmt.Errorf("must not start with '-'")
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			return fmt.Errorf("must not contain slashes or control characters")
		}
	}
	return nil
}

// dumpCommand returns the command that dumps one database to its stdout.
// The database name is preceded by "--" so that getopt-style option
// parsing in mysqldump/pg_dump can never mistake it for a flag.
func (e *Engine) dumpCommand(ctx context.Context, job *config.Job, db string) (*exec.Cmd, error) {
	switch job.Type {
	case config.JobMySQL:
		args, menv := mysqlArgs(job)
		args = append(args, "--", db)
		cmd := exec.CommandContext(ctx, "mysqldump", args...)
		if len(menv) > 0 {
			cmd.Env = envList(os.Environ(), menv)
		}
		return cmd, nil
	case config.JobPostgreSQL:
		args, penv := pgArgs(job)
		args = append(args, "--", db)
		cmd := exec.CommandContext(ctx, "pg_dump", args...)
		if len(penv) > 0 {
			cmd.Env = envList(os.Environ(), penv)
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("job %q: unsupported database type %q", job.Name, job.Type)
}
