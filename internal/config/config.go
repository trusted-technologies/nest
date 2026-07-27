// Package config defines the nest configuration model and its (un)loading.
//
// The configuration lives in /etc/nest/config.yml (mode 0600) and describes
// a set of remotes (where backups go) and jobs (what is backed up).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultPath is the default configuration file location.
	DefaultPath = "/etc/nest/config.yml"
	// Dir is the configuration directory (also holds the SSH keypair).
	Dir = "/etc/nest"
	// StateDir holds nest-managed binaries and runtime state.
	StateDir = "/var/lib/nest"
	// CacheDir is used as restic cache location (XDG_CACHE_HOME).
	CacheDir = "/var/cache/nest"
)

// Remote types.
const (
	RemoteS3     = "s3"
	RemoteSFTP   = "sftp"
	RemoteRclone = "rclone"
)

// Job types.
const (
	JobFolder     = "folder"
	JobMySQL      = "mysql"
	JobPostgreSQL = "postgresql"
)

// Config is the root of /etc/nest/config.yml.
type Config struct {
	Version int `yaml:"version"`

	// EncryptionPassword is the default restic repository password.
	// Generated once during setup; individual jobs may override it.
	EncryptionPassword string `yaml:"encryption_password"`

	Remotes []Remote `yaml:"remotes"`
	Jobs    []Job    `yaml:"jobs"`

	Notifications Notifications `yaml:"notifications,omitempty"`
}

// Remote describes a backup destination.
type Remote struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // s3 | sftp | rclone

	// s3 (any S3-compatible storage: AWS, Wasabi, MinIO, R2, ...)
	Endpoint  string `yaml:"endpoint,omitempty"`
	Region    string `yaml:"region,omitempty"`
	Bucket    string `yaml:"bucket,omitempty"`
	AccessKey string `yaml:"access_key,omitempty"`
	SecretKey string `yaml:"secret_key,omitempty"`

	// sftp (any SSH server; restic talks to it natively)
	Host    string `yaml:"host,omitempty"`
	Port    int    `yaml:"port,omitempty"`
	User    string `yaml:"user,omitempty"`
	KeyFile string `yaml:"key_file,omitempty"`

	// rclone (uses the user's `rclone config` remotes, e.g. "gdrive:backups")
	RcloneRemote string `yaml:"rclone_remote,omitempty"`

	// PathPrefix is prepended to every repository/archive path of this
	// remote (useful to separate several servers inside one bucket).
	PathPrefix string `yaml:"path_prefix,omitempty"`
}

// Job describes a single backup object.
type Job struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"` // folder | mysql | postgresql
	Preset string `yaml:"preset,omitempty"`

	// Source is the directory to back up (folder jobs).
	Source string `yaml:"source,omitempty"`

	// Remote references Remotes[].Name.
	Remote string `yaml:"remote"`

	// Incremental selects the backend: true = restic (incremental,
	// deduplicated, encrypted), false = plain .tar.gz archives.
	Incremental bool `yaml:"incremental"`

	// Schedule is either "HH:MM" (daily) or a 5-field cron expression.
	// Empty means "run on every timer tick".
	Schedule string `yaml:"schedule,omitempty"`

	Retention Retention `yaml:"retention,omitempty"`
	Exclude   []string  `yaml:"exclude,omitempty"`
	Enabled   bool      `yaml:"enabled"`

	// ResticPassword overrides Config.EncryptionPassword for this job.
	ResticPassword string `yaml:"restic_password,omitempty"`

	// Database jobs (mysql / postgresql). Empty Databases = all databases.
	DBHost     string   `yaml:"db_host,omitempty"`
	DBPort     int      `yaml:"db_port,omitempty"`
	DBUser     string   `yaml:"db_user,omitempty"`
	DBPassword string   `yaml:"db_password,omitempty"`
	Databases  []string `yaml:"databases,omitempty"`
}

// Retention configures how many snapshots to keep.
type Retention struct {
	Last    int `yaml:"last,omitempty"`
	Daily   int `yaml:"daily,omitempty"`
	Weekly  int `yaml:"weekly,omitempty"`
	Monthly int `yaml:"monthly,omitempty"`
}

// Notifications holds notifier configurations.
type Notifications struct {
	Telegram Telegram `yaml:"telegram,omitempty"`
}

// Telegram configures a Telegram bot notifier.
type Telegram struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token,omitempty"`
	ChatID   string `yaml:"chat_id,omitempty"`
	ThreadID string `yaml:"thread_id,omitempty"`
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

// LoadOrEmpty returns the config from path, or an empty initialized one
// when the file does not exist yet.
func LoadOrEmpty(path string) (*Config, error) {
	cfg, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Version: 1}, nil
	}
	return cfg, err
}

// Save writes the configuration atomically with mode 0600.
func Save(path string, cfg *Config) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FindRemote returns the remote with the given name, or nil.
func (c *Config) FindRemote(name string) *Remote {
	for i := range c.Remotes {
		if c.Remotes[i].Name == name {
			return &c.Remotes[i]
		}
	}
	return nil
}

// FindJob returns the job with the given name, or nil.
func (c *Config) FindJob(name string) *Job {
	for i := range c.Jobs {
		if c.Jobs[i].Name == name {
			return &c.Jobs[i]
		}
	}
	return nil
}

// EnabledJobs returns all jobs with Enabled == true.
func (c *Config) EnabledJobs() []Job {
	var out []Job
	for _, j := range c.Jobs {
		if j.Enabled {
			out = append(out, j)
		}
	}
	return out
}

// Validate checks the configuration for consistency and returns all
// problems found (empty slice = valid).
func (c *Config) Validate() []string {
	var errs []string
	if len(c.Remotes) == 0 {
		errs = append(errs, "no remotes configured (add one via `nest setup`)")
	}
	if len(c.Jobs) == 0 {
		errs = append(errs, "no backup jobs configured (add one via `nest setup`)")
	}
	remoteNames := map[string]bool{}
	for _, r := range c.Remotes {
		if r.Name == "" {
			errs = append(errs, "remote with empty name")
		}
		if remoteNames[r.Name] {
			errs = append(errs, fmt.Sprintf("duplicate remote name %q", r.Name))
		}
		remoteNames[r.Name] = true
		switch r.Type {
		case RemoteS3:
			if r.Bucket == "" {
				errs = append(errs, fmt.Sprintf("remote %q: bucket is required", r.Name))
			}
			if r.AccessKey == "" || r.SecretKey == "" {
				errs = append(errs, fmt.Sprintf("remote %q: access_key and secret_key are required", r.Name))
			}
		case RemoteSFTP:
			if r.Host == "" || r.User == "" {
				errs = append(errs, fmt.Sprintf("remote %q: host and user are required", r.Name))
			}
		case RemoteRclone:
			if r.RcloneRemote == "" {
				errs = append(errs, fmt.Sprintf("remote %q: rclone_remote is required", r.Name))
			}
		default:
			errs = append(errs, fmt.Sprintf("remote %q: unknown type %q (want s3, sftp or rclone)", r.Name, r.Type))
		}
	}
	jobNames := map[string]bool{}
	for _, j := range c.Jobs {
		prefix := fmt.Sprintf("job %q:", j.Name)
		if j.Name == "" {
			errs = append(errs, "job with empty name")
		}
		if jobNames[j.Name] {
			errs = append(errs, fmt.Sprintf("duplicate job name %q", j.Name))
		}
		jobNames[j.Name] = true
		switch j.Type {
		case JobFolder:
			if j.Source == "" {
				errs = append(errs, prefix+" source is required")
			}
		case JobMySQL, JobPostgreSQL:
		default:
			errs = append(errs, fmt.Sprintf("%s unknown type %q (want folder, mysql or postgresql)", prefix, j.Type))
		}
		if !remoteNames[j.Remote] {
			errs = append(errs, fmt.Sprintf("%s unknown remote %q", prefix, j.Remote))
		}
		if j.Incremental && c.EncryptionPassword == "" && j.ResticPassword == "" && os.Getenv("NEST_RESTIC_PASSWORD") == "" {
			errs = append(errs, prefix+" incremental backups need encryption_password (config), restic_password (job) or NEST_RESTIC_PASSWORD")
		}
	}
	return errs
}
