// nest — a small server backup tool. See usageText for the command tree.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/trusted-technologies/nest/internal/buildinfo"
	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/engine"
	"github.com/trusted-technologies/nest/internal/selfupdate"
	"github.com/trusted-technologies/nest/internal/tui"
)

const usageText = `nest — server backup tool

Usage:
  nest [global flags] <command> [command flags]

Global flags:
  -c, --config PATH   config file (default ` + config.DefaultPath + `)
  -v, --verbose       verbose (debug) logging

Commands:
  (none), setup, tui    interactive setup & management UI
  backup                run backup jobs
      --job NAME          run only this job (default: all enabled jobs)
      --scheduled         run only jobs whose schedule is due
      --window-minutes N  schedule tolerance window in minutes (default 5)
  restore               restore a restic snapshot
      --job NAME          job to restore (required)
      --snapshot ID       snapshot to restore (default "latest")
      --target DIR        restore destination (default /var/lib/nest/restore)
      --path P            restore only files matching P
  jobs                  list configured backup jobs
  remotes               list configured remotes
  snapshots             list restic snapshots (--job NAME, required)
  repair                repair a restic repository
      --job NAME          job whose repository to repair (required)
      --full              also run restic repair index/snapshots
  version               print version
  self-update           update the nest binary from the mirror

Examples:
  nest setup
  nest backup --job files
  nest backup --scheduled
  nest restore --job files --snapshot latest --target /tmp/restore
  nest snapshots --job files
  nest repair --job files --full
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("nest", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usageText) }
	configPath := config.DefaultPath
	verbose := false
	fs.StringVar(&configPath, "config", config.DefaultPath, "config file path")
	fs.StringVar(&configPath, "c", config.DefaultPath, "config file path (shorthand)")
	fs.BoolVar(&verbose, "verbose", false, "verbose logging")
	fs.BoolVar(&verbose, "v", false, "verbose logging (shorthand)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	rest := fs.Args()
	cmd := ""
	if len(rest) > 0 {
		cmd, rest = rest[0], rest[1:]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "", "setup", "tui":
		_, statErr := os.Stat(configPath)
		firstInstall := cmd == "setup" && errors.Is(statErr, os.ErrNotExist)
		return runErr(tui.Run(configPath, firstInstall))

	case "backup":
		f := newFlagSet("backup")
		job := f.String("job", "", "run only this job")
		scheduled := f.Bool("scheduled", false, "run only jobs whose schedule is due")
		window := f.Int("window-minutes", 5, "schedule tolerance window in minutes")
		if code := parseFlags(f, rest); code >= 0 {
			return code
		}
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		return runErr(eng.RunBackup(ctx, *job, *scheduled, time.Duration(*window)*time.Minute))

	case "restore":
		f := newFlagSet("restore")
		job := f.String("job", "", "job to restore")
		snapshot := f.String("snapshot", "latest", "snapshot to restore")
		target := f.String("target", "/var/lib/nest/restore", "restore destination")
		path := f.String("path", "", "restore only files matching this path")
		if code := parseFlags(f, rest); code >= 0 {
			return code
		}
		if *job == "" {
			return fail(fmt.Errorf("restore requires --job NAME"))
		}
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		return runErr(eng.RunRestore(ctx, *job, *snapshot, *target, *path))

	case "jobs":
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		eng.ListJobs()
		return 0

	case "remotes":
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		eng.ListRemotes()
		return 0

	case "snapshots":
		f := newFlagSet("snapshots")
		job := f.String("job", "", "job whose snapshots to list")
		if code := parseFlags(f, rest); code >= 0 {
			return code
		}
		if *job == "" {
			return fail(fmt.Errorf("snapshots requires --job NAME"))
		}
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		return runErr(eng.ListSnapshots(ctx, *job))

	case "repair":
		f := newFlagSet("repair")
		job := f.String("job", "", "job whose repository to repair")
		full := f.Bool("full", false, "also run restic repair index/snapshots")
		if code := parseFlags(f, rest); code >= 0 {
			return code
		}
		if *job == "" {
			return fail(fmt.Errorf("repair requires --job NAME"))
		}
		eng, code := newEngine(configPath, verbose)
		if code >= 0 {
			return code
		}
		return runErr(eng.Repair(ctx, *job, *full))

	case "version":
		fmt.Printf("nest %s\n", buildinfo.Version)
		return 0

	case "self-update":
		return runErr(selfupdate.Run(ctx))

	case "help":
		fmt.Print(usageText)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		fmt.Fprint(os.Stderr, usageText)
		return 1
	}
}

// newFlagSet creates a FlagSet for a subcommand.
func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet("nest "+name, flag.ContinueOnError)
}

// parseFlags parses subcommand flags; it returns an exit code (>= 0) when
// parsing finished the command (help shown or error printed), or -1 to
// continue.
func parseFlags(f *flag.FlagSet, args []string) int {
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	return -1
}

// newEngine loads and validates the config and builds an Engine. It
// returns an exit code (>= 0) on failure.
func newEngine(configPath string, verbose bool) (*engine.Engine, int) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fail(err)
		return nil, 1
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "invalid configuration %s:\n", configPath)
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return nil, 1
	}
	eng := engine.New(cfg)
	eng.Verbose = verbose
	return eng, -1
}

// fail prints err to stderr and returns exit code 1.
func fail(err error) int {
	fmt.Fprintln(os.Stderr, "error:", err)
	return 1
}

// runErr is fail for nullable errors.
func runErr(err error) int {
	if err != nil {
		return fail(err)
	}
	return 0
}
