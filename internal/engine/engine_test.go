package engine

import (
	"reflect"
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
