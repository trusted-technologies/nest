package remote

import (
	"testing"

	"github.com/trusted-technologies/nest/internal/config"
)

func TestResticRepository(t *testing.T) {
	cases := []struct {
		name    string
		remote  config.Remote
		job     string
		want    string
		wantErr bool
	}{
		{
			name:   "s3 https endpoint without scheme gets https",
			remote: config.Remote{Type: config.RemoteS3, Endpoint: "s3.eu-central-2.wasabisys.com", Bucket: "b"},
			job:    "j",
			want:   "s3:https://s3.eu-central-2.wasabisys.com/b/nest/j",
		},
		{
			name:   "s3 explicit http endpoint keeps http (e.g. local MinIO)",
			remote: config.Remote{Type: config.RemoteS3, Endpoint: "http://minio:9000", Bucket: "b"},
			job:    "j",
			want:   "s3:http://minio:9000/b/nest/j",
		},
		{
			name:   "s3 empty endpoint defaults to AWS",
			remote: config.Remote{Type: config.RemoteS3, Bucket: "b"},
			job:    "j",
			want:   "s3:https://s3.amazonaws.com/b/nest/j",
		},
		{
			name:   "s3 path prefix",
			remote: config.Remote{Type: config.RemoteS3, Endpoint: "e.example.com", Bucket: "b", PathPrefix: "web-1"},
			job:    "j",
			want:   "s3:https://e.example.com/b/web-1/nest/j",
		},
		{
			name:   "sftp default port",
			remote: config.Remote{Type: config.RemoteSFTP, Host: "h.example.com", User: "backup"},
			job:    "j",
			want:   "sftp:backup@h.example.com:22/nest/j",
		},
		{
			name:   "sftp absolute path prefix",
			remote: config.Remote{Type: config.RemoteSFTP, Host: "h", User: "u", Port: 2222, PathPrefix: "/srv/backups"},
			job:    "j",
			want:   "sftp:u@h:2222/srv/backups/nest/j",
		},
		{
			name:   "rclone passthrough",
			remote: config.Remote{Type: config.RemoteRclone, RcloneRemote: "gdrive:backups"},
			job:    "j",
			want:   "rclone:gdrive:backups/nest/j",
		},
		{
			name:    "unknown type",
			remote:  config.Remote{Type: "nope"},
			job:     "j",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResticRepository(&tc.remote, tc.job)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestArchiveTargetS3(t *testing.T) {
	r := &config.Remote{
		Type: config.RemoteS3, Endpoint: "https://s3.example.com",
		Bucket: "b", AccessKey: "ak", SecretKey: "sk",
	}
	target, env, err := ArchiveTarget(r, "job1")
	if err != nil {
		t.Fatal(err)
	}
	if target != "nest:b/nest-archive/job1" {
		t.Errorf("unexpected target %q", target)
	}
	if env["RCLONE_CONFIG_NEST_TYPE"] != "s3" || env["RCLONE_CONFIG_NEST_ENDPOINT"] != "https://s3.example.com" {
		t.Errorf("unexpected env: %v", env)
	}
	if _, ok := env["RCLONE_CONFIG_NEST_REGION"]; ok {
		t.Errorf("empty region must be omitted, got %v", env)
	}
}
