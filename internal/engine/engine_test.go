package engine

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/trusted-technologies/nest/internal/config"
)

func TestArchiveName(t *testing.T) {
	ts := time.Date(2026, 7, 27, 15, 4, 5, 0, time.UTC)
	if got := archiveName(ts); got != "20260727-150405.tar.gz" {
		t.Errorf("got %q", got)
	}
}

func TestRetentionDelete(t *testing.T) {
	names := []string{"a.tar.gz", "b.tar.gz", "c.tar.gz", "d.tar.gz"}
	if got := retentionDelete(names, 2); !reflect.DeepEqual(got, []string{"a.tar.gz", "b.tar.gz"}) {
		t.Errorf("keep 2: got %v", got)
	}
	if got := retentionDelete(names, 4); got != nil {
		t.Errorf("keep all: got %v", got)
	}
	if got := retentionDelete(names, 10); got != nil {
		t.Errorf("keep more than exist: got %v", got)
	}
	if got := retentionDelete(names, 0); !reflect.DeepEqual(got, []string{"a.tar.gz", "b.tar.gz", "c.tar.gz"}) {
		t.Errorf("keep 0 clamps to 1: got %v", got)
	}
}

func TestIsSystemDB(t *testing.T) {
	for _, db := range []string{"information_schema", "mysql", "performance_schema", "sys", "template0", "template1", "postgres"} {
		if !isSystemDB(db) {
			t.Errorf("%s should be a system db", db)
		}
	}
	for _, db := range []string{"app", "wordpress", "postgres2", "mydb"} {
		if isSystemDB(db) {
			t.Errorf("%s should not be a system db", db)
		}
	}
}

func TestDBDefaults(t *testing.T) {
	host, port, user := dbDefaults(&config.Job{Type: config.JobMySQL})
	if host != "localhost" || port != 3306 || user != "root" {
		t.Errorf("mysql defaults: got %s %d %s", host, port, user)
	}
	host, port, user = dbDefaults(&config.Job{Type: config.JobPostgreSQL})
	if host != "localhost" || port != 5432 || user != "postgres" {
		t.Errorf("pg defaults: got %s %d %s", host, port, user)
	}
	host, port, user = dbDefaults(&config.Job{Type: config.JobMySQL, DBHost: "db.example", DBPort: 3307, DBUser: "backup"})
	if host != "db.example" || port != 3307 || user != "backup" {
		t.Errorf("explicit values: got %s %d %s", host, port, user)
	}
}

func TestMysqlArgs(t *testing.T) {
	// localhost: unix socket, no -h/-P (same as `sudo mysql`)
	args, env := mysqlArgs(&config.Job{Type: config.JobMySQL})
	if want := []string{"-u", "root"}; !reflect.DeepEqual(args, want) {
		t.Errorf("localhost: got %v, want %v", args, want)
	}
	if len(env) != 0 {
		t.Errorf("localhost without password: unexpected env %v", env)
	}
	// localhost + password: socket, password travels via MYSQL_PWD — never argv
	args, env = mysqlArgs(&config.Job{Type: config.JobMySQL, DBPassword: "pw"})
	if want := []string{"-u", "root"}; !reflect.DeepEqual(args, want) {
		t.Errorf("localhost+pw: got %v, want %v", args, want)
	}
	if env["MYSQL_PWD"] != "pw" {
		t.Errorf("localhost+pw: MYSQL_PWD env missing, got %v", env)
	}
	for _, a := range args {
		if strings.Contains(a, "pw") {
			t.Errorf("password must not appear in argv: %v", args)
		}
	}
	// remote host: TCP with -h/-P
	args, _ = mysqlArgs(&config.Job{Type: config.JobMySQL, DBHost: "db.example", DBUser: "backup"})
	if want := []string{"-h", "db.example", "-P", "3306", "-u", "backup"}; !reflect.DeepEqual(args, want) {
		t.Errorf("remote: got %v, want %v", args, want)
	}
	// explicit 127.0.0.1: TCP
	args, _ = mysqlArgs(&config.Job{Type: config.JobMySQL, DBHost: "127.0.0.1"})
	if want := []string{"-h", "127.0.0.1", "-P", "3306", "-u", "root"}; !reflect.DeepEqual(args, want) {
		t.Errorf("127.0.0.1: got %v, want %v", args, want)
	}
}

func TestValidDBName(t *testing.T) {
	for _, ok := range []string{"app", "wordpress2", "my-db", "db.name", "s22748_StarLand", "база"} {
		if err := validDBName(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "--file=/root/x", "-f/etc/shadow", "--result-file=vmlinuz",
		"../../etc/cron.d/pwn", "a/b", `a\b`, "line\nbreak", "tab\there", "del\x7f",
	} {
		if err := validDBName(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
