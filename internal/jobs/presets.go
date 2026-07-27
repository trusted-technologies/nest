// Package jobs provides ready-made job templates (presets) for common
// backup objects. The TUI offers them when creating a new job; the preset
// only pre-fills fields, everything stays editable.
package jobs

import "github.com/trusted-technologies/nest/internal/config"

// Preset is a job template shown in the "add job" dialog.
type Preset struct {
	ID          string
	Title       string
	Description string
	Job         config.Job
}

// Presets lists all built-in job templates.
func Presets() []Preset {
	return []Preset{
		{
			ID:          "pterodactyl-volumes",
			Title:       "Pterodactyl Volumes",
			Description: "Game server data (/var/lib/pterodactyl/volumes)",
			Job: config.Job{
				Name:        "pterodactyl-volumes",
				Type:        config.JobFolder,
				Preset:      "pterodactyl-volumes",
				Source:      "/var/lib/pterodactyl/volumes",
				Incremental: true,
				Schedule:    "04:00",
				Retention:   config.Retention{Daily: 3, Weekly: 1},
				Enabled:     true,
			},
		},
		{
			ID:          "pterodactyl-backups",
			Title:       "Pterodactyl Backups",
			Description: "Panel-created backups (/var/lib/pterodactyl/backups)",
			Job: config.Job{
				Name:        "pterodactyl-backups",
				Type:        config.JobFolder,
				Preset:      "pterodactyl-backups",
				Source:      "/var/lib/pterodactyl/backups",
				Incremental: true,
				Schedule:    "05:00",
				Retention:   config.Retention{Last: 7},
				Enabled:     true,
			},
		},
		{
			ID:          "folder",
			Title:       "Folder",
			Description: "Any directory (websites, configs, app data)",
			Job: config.Job{
				Name:        "folder",
				Type:        config.JobFolder,
				Source:      "/var/www",
				Incremental: true,
				Schedule:    "04:00",
				Retention:   config.Retention{Last: 7},
				Enabled:     true,
			},
		},
		{
			ID:          "mysql",
			Title:       "MySQL / MariaDB",
			Description: "Database dumps streamed to storage (mysqldump)",
			Job: config.Job{
				Name:        "mysql",
				Type:        config.JobMySQL,
				Incremental: true,
				Schedule:    "03:00",
				Retention:   config.Retention{Daily: 7},
				Enabled:     true,
				DBHost:      "localhost",
				DBPort:      3306,
				DBUser:      "root",
			},
		},
		{
			ID:          "postgresql",
			Title:       "PostgreSQL",
			Description: "Database dumps streamed to storage (pg_dump)",
			Job: config.Job{
				Name:        "postgresql",
				Type:        config.JobPostgreSQL,
				Incremental: true,
				Schedule:    "03:30",
				Retention:   config.Retention{Daily: 7},
				Enabled:     true,
				DBHost:      "localhost",
				DBPort:      5432,
				DBUser:      "postgres",
			},
		},
	}
}
