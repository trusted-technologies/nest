// Package remote translates config.Remote definitions into concrete
// connection parameters for restic (incremental backups) and rclone
// (plain archive uploads).
package remote

import (
	"fmt"
	"path"
	"strings"

	"github.com/trusted-technologies/nest/internal/config"
)

// joinPrefix joins path fragments, dropping empty ones, duplicate slashes
// and any leading slash (rclone/s3 targets must be relative).
func joinPrefix(parts ...string) string {
	return strings.TrimPrefix(path.Join(parts...), "/")
}

// normalizeEndpoint ensures the endpoint has an https:// scheme.
func normalizeEndpoint(ep string) string {
	if ep == "" {
		return "https://s3.amazonaws.com"
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		return "https://" + ep
	}
	return ep
}

// ResticRepository returns the restic -r repository URL for a job on a remote.
func ResticRepository(r *config.Remote, jobName string) (string, error) {
	base := joinPrefix(r.PathPrefix, "nest", jobName)
	switch r.Type {
	case config.RemoteS3:
		// restic accepts s3:https://endpoint/bucket/path (https by default,
		// plain http only when explicitly given).
		return fmt.Sprintf("s3:%s/%s/%s", normalizeEndpoint(r.Endpoint), r.Bucket, base), nil
	case config.RemoteSFTP:
		port := r.Port
		if port == 0 {
			port = 22
		}
		// restic sftp syntax: sftp:user@host:port/path — always absolute,
		// so an absolute path_prefix keeps working unchanged.
		return fmt.Sprintf("sftp:%s@%s:%d/%s", r.User, r.Host, port, base), nil
	case config.RemoteRclone:
		return fmt.Sprintf("rclone:%s/%s", strings.TrimSuffix(r.RcloneRemote, "/"), base), nil
	}
	return "", fmt.Errorf("remote %q: unsupported type %q", r.Name, r.Type)
}

// ResticEnv returns extra environment variables restic needs for a remote.
func ResticEnv(r *config.Remote) (map[string]string, error) {
	env := map[string]string{}
	switch r.Type {
	case config.RemoteS3:
		env["AWS_ACCESS_KEY_ID"] = r.AccessKey
		env["AWS_SECRET_ACCESS_KEY"] = r.SecretKey
		region := r.Region
		if region == "" {
			region = "us-east-1"
		}
		env["AWS_DEFAULT_REGION"] = region
	case config.RemoteSFTP:
		// authentication handled via ResticOptions (ssh key)
	case config.RemoteRclone:
		// uses the user's own rclone.conf
	default:
		return nil, fmt.Errorf("remote %q: unsupported type %q", r.Name, r.Type)
	}
	return env, nil
}

// ResticOptions returns extra CLI flags ("-o", "key=value") for restic.
func ResticOptions(r *config.Remote, sshDir string) []string {
	if r.Type != config.RemoteSFTP {
		return nil
	}
	keyFile := r.KeyFile
	if keyFile == "" {
		keyFile = DefaultKeyPath(sshDir)
	}
	args := fmt.Sprintf("-i %s -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=%s/known_hosts -o BatchMode=yes",
		keyFile, sshDir)
	return []string{"-o", "sftp.args=" + args}
}

// ArchiveTarget returns the rclone target (remote:path) and the environment
// variables needed to upload a plain .tar.gz archive for a job.
//
// For s3/sftp remotes an on-the-fly rclone remote named "nest" is built
// purely from environment variables (no rclone.conf needed). For rclone
// remotes the user's configured remote is used as-is.
func ArchiveTarget(r *config.Remote, jobName string) (target string, env map[string]string, err error) {
	base := joinPrefix(r.PathPrefix, "nest-archive", jobName)
	switch r.Type {
	case config.RemoteS3:
		env = map[string]string{
			"RCLONE_CONFIG_NEST_TYPE":                   "s3",
			"RCLONE_CONFIG_NEST_PROVIDER":               "Other",
			"RCLONE_CONFIG_NEST_ACCESS_KEY_ID":          r.AccessKey,
			"RCLONE_CONFIG_NEST_SECRET_ACCESS_KEY":      r.SecretKey,
			"RCLONE_CONFIG_NEST_ENDPOINT":               normalizeEndpoint(r.Endpoint),
			"RCLONE_CONFIG_NEST_REGION":                 r.Region,
			"RCLONE_CONFIG_NEST_SERVER_SIDE_ENCRYPTION": "",
		}
		if r.Region == "" {
			delete(env, "RCLONE_CONFIG_NEST_REGION")
		}
		return fmt.Sprintf("nest:%s/%s", r.Bucket, base), env, nil
	case config.RemoteSFTP:
		port := r.Port
		if port == 0 {
			port = 22
		}
		keyFile := r.KeyFile
		env = map[string]string{
			"RCLONE_CONFIG_NEST_TYPE": "sftp",
			"RCLONE_CONFIG_NEST_HOST": r.Host,
			"RCLONE_CONFIG_NEST_USER": r.User,
			"RCLONE_CONFIG_NEST_PORT": fmt.Sprintf("%d", port),
		}
		if keyFile != "" {
			env["RCLONE_CONFIG_NEST_KEY_FILE"] = keyFile
		}
		path := base
		if strings.HasPrefix(r.PathPrefix, "/") {
			path = "/" + base
		}
		return "nest:" + path, env, nil
	case config.RemoteRclone:
		return fmt.Sprintf("%s/%s", strings.TrimSuffix(r.RcloneRemote, "/"), base), map[string]string{}, nil
	}
	return "", nil, fmt.Errorf("remote %q: unsupported type %q", r.Name, r.Type)
}
