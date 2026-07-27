package tui

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/remote"
)

// remotesMenu is the Remotes submenu loop.
func (a *app) remotesMenu() error {
	for {
		var action string
		err := runFields(
			huh.NewNote().Title("Remotes").Description(a.remoteList()),
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Add a remote", "add"),
					huh.NewOption("Edit a remote", "edit"),
					huh.NewOption("Delete a remote", "delete"),
					huh.NewOption("Test connection", "test"),
					huh.NewOption("Back", "back"),
				).
				Value(&action))
		if aborted(err) {
			return nil
		}
		if err != nil {
			return err
		}

		switch action {
		case "add":
			err = a.addRemote()
		case "edit":
			err = a.editRemote()
		case "delete":
			err = a.deleteRemote()
		case "test":
			err = a.testRemote()
		case "back":
			return nil
		}
		if err != nil && !aborted(err) {
			return err
		}
	}
}

// remoteList renders the remotes overview shown above the submenu.
func (a *app) remoteList() string {
	if len(a.cfg.Remotes) == 0 {
		return "No remotes configured yet."
	}
	var sb strings.Builder
	for i := range a.cfg.Remotes {
		fmt.Fprintf(&sb, "• %s\n", remoteLabel(&a.cfg.Remotes[i]))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// remoteLabel is the one-line "name [type] summary" form of a remote.
func remoteLabel(r *config.Remote) string {
	return fmt.Sprintf("%s [%s] %s", r.Name, r.Type, remoteSummary(r))
}

// remoteSummary renders the type-specific part of a remote label.
func remoteSummary(r *config.Remote) string {
	switch r.Type {
	case config.RemoteS3:
		return strings.TrimSuffix(r.Endpoint, "/") + "/" + r.Bucket
	case config.RemoteSFTP:
		port := r.Port
		if port == 0 {
			port = 22
		}
		return fmt.Sprintf("%s@%s:%d", r.User, r.Host, port)
	case config.RemoteRclone:
		return r.RcloneRemote
	default:
		return ""
	}
}

// pickRemote asks the user to choose a remote and returns its index
// (-1 when there are none).
func (a *app) pickRemote(title string) (int, error) {
	if len(a.cfg.Remotes) == 0 {
		return -1, nil
	}
	opts := make([]huh.Option[int], 0, len(a.cfg.Remotes))
	for i := range a.cfg.Remotes {
		opts = append(opts, huh.NewOption(remoteLabel(&a.cfg.Remotes[i]), i))
	}
	idx := 0
	if err := runFields(huh.NewSelect[int]().Title(title).Options(opts...).Value(&idx)); err != nil {
		return -1, err
	}
	return idx, nil
}

// uniqueRemoteName validates that a remote name is non-empty and unique,
// excluding origName (the name before an edit).
func (a *app) uniqueRemoteName(origName string) func(string) error {
	return func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return fmt.Errorf("name is required")
		}
		if s != origName && a.cfg.FindRemote(s) != nil {
			return fmt.Errorf("a remote named %q already exists", s)
		}
		return nil
	}
}

// addRemote asks for a type and runs the matching remote form.
func (a *app) addRemote() error {
	var typ string
	err := runFields(huh.NewSelect[string]().
		Title("Remote type").
		Options(
			huh.NewOption("S3-compatible (AWS, Wasabi, MinIO, Cloudflare R2, ...)", config.RemoteS3),
			huh.NewOption("SSH (SFTP) — any server you can SSH into", config.RemoteSFTP),
			huh.NewOption("rclone — use an existing `rclone config` remote", config.RemoteRclone),
		).
		Value(&typ))
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}

	r := config.Remote{Type: typ, Port: 22}
	if err := a.remoteForm(&r, ""); err != nil {
		if aborted(err) {
			return nil
		}
		return err
	}
	a.cfg.Remotes = append(a.cfg.Remotes, r)
	a.dirty = true
	if r.Type == config.RemoteSFTP {
		a.showSSHKey(&a.cfg.Remotes[len(a.cfg.Remotes)-1])
	}
	return nil
}

// editRemote edits a copy of the chosen remote and writes it back on
// success. Jobs are retargeted when the remote is renamed.
func (a *app) editRemote() error {
	idx, err := a.pickRemote("Edit which remote?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No remotes", "Add a remote first.")
		return nil
	}
	origName := a.cfg.Remotes[idx].Name
	r := a.cfg.Remotes[idx]
	if err := a.remoteForm(&r, origName); err != nil {
		if aborted(err) {
			return nil
		}
		return err
	}
	a.cfg.Remotes[idx] = r
	if r.Name != origName {
		for i := range a.cfg.Jobs {
			if a.cfg.Jobs[i].Remote == origName {
				a.cfg.Jobs[i].Remote = r.Name
			}
		}
	}
	a.dirty = true
	return nil
}

// deleteRemote removes a remote after confirmation, warning about jobs
// that still reference it.
func (a *app) deleteRemote() error {
	idx, err := a.pickRemote("Delete which remote?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No remotes", "Nothing to delete.")
		return nil
	}
	r := &a.cfg.Remotes[idx]
	var users []string
	for _, j := range a.cfg.Jobs {
		if j.Remote == r.Name {
			users = append(users, j.Name)
		}
	}
	desc := ""
	if len(users) > 0 {
		desc = "Warning: these jobs use this remote and will stop working:\n" + strings.Join(users, ", ")
	}
	ok, err := askConfirm(fmt.Sprintf("Delete remote %q?", r.Name), desc, false)
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	a.cfg.Remotes = slices.Delete(a.cfg.Remotes, idx, idx+1)
	a.dirty = true
	return nil
}

// remoteForm runs the editor for r (prefilled). origName is excluded from
// the name-uniqueness check ("" when adding a new remote).
func (a *app) remoteForm(r *config.Remote, origName string) error {
	fields := []huh.Field{
		huh.NewInput().
			Title("Name").
			Description("Unique name; jobs reference the remote by it.").
			Value(&r.Name).
			Validate(a.uniqueRemoteName(origName)),
	}
	port := ""
	switch r.Type {
	case config.RemoteS3:
		fields = append(fields,
			huh.NewInput().Title("Endpoint").Placeholder("s3.eu-central-1.amazonaws.com").Value(&r.Endpoint).Validate(validateRequired),
			huh.NewInput().Title("Region (optional)").Placeholder("eu-central-1").Value(&r.Region),
			huh.NewInput().Title("Bucket").Value(&r.Bucket).Validate(validateRequired),
			huh.NewInput().Title("Access key").Value(&r.AccessKey).Validate(validateRequired),
			huh.NewInput().Title("Secret key").Password(true).Value(&r.SecretKey).Validate(validateRequired),
		)
	case config.RemoteSFTP:
		if r.Port <= 0 {
			r.Port = 22
		}
		port = strconv.Itoa(r.Port)
		fields = append(fields,
			huh.NewInput().Title("Host").Placeholder("backup.example.com").Value(&r.Host).Validate(validateRequired),
			huh.NewInput().Title("Port").Value(&port).Validate(validatePort),
			huh.NewInput().Title("User").Value(&r.User).Validate(validateRequired),
		)
	case config.RemoteRclone:
		fields = append(fields,
			huh.NewNote().
				Title("rclone remote").
				Description("nest uses your existing rclone configuration.\nIf the remote does not exist yet, quit and run `rclone config` first."),
			huh.NewInput().Title("Rclone remote").Placeholder("gdrive:backups").Value(&r.RcloneRemote).Validate(validateRequired),
		)
	}
	fields = append(fields,
		huh.NewInput().
			Title("Path prefix (optional)").
			Description("Prepended to every repository/archive path on this remote.").
			Placeholder("server1").
			Value(&r.PathPrefix),
	)
	if err := runFields(fields...); err != nil {
		return err
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Type == config.RemoteSFTP {
		r.Port, _ = strconv.Atoi(strings.TrimSpace(port))
		if r.KeyFile == "" {
			r.KeyFile = remote.DefaultKeyPath(filepath.Join(config.Dir, "ssh"))
		}
	}
	return nil
}

// showSSHKey ensures the nest SSH keypair exists and displays the public
// key with installation instructions.
func (a *app) showSSHKey(r *config.Remote) {
	sshDir := filepath.Join(config.Dir, "ssh")
	if _, _, err := remote.EnsureKeyPair(sshDir); err != nil {
		showNote("SSH keypair",
			"Could not create the nest SSH keypair in "+sshDir+":\n\n"+err.Error()+
				"\n\nFix the permissions (e.g. run nest as root) and re-add the remote.")
		return
	}
	pub := strings.TrimSpace(remote.PublicKey(sshDir))
	showNote("Authorize the backup key",
		fmt.Sprintf("nest connects to %s@%s with this public key.\nAdd it to ~/.ssh/authorized_keys of that user on the backup server:\n\n%s",
			r.User, r.Host, pub))
}

// testRemote runs a lightweight reachability check for the chosen remote.
func (a *app) testRemote() error {
	idx, err := a.pickRemote("Test which remote?")
	if aborted(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if idx < 0 {
		showNote("No remotes", "Add a remote first.")
		return nil
	}
	msg, ok := checkRemote(&a.cfg.Remotes[idx])
	title := "Connection OK"
	if !ok {
		title = "Connection failed"
	}
	showNote(title, msg)
	return nil
}

// checkRemote performs a dependency-free reachability check: TCP dial for
// SFTP, HTTPS GET for S3, rclone binary presence for rclone remotes.
// Credentials are not verified — that happens on the first real backup run.
func checkRemote(r *config.Remote) (msg string, ok bool) {
	switch r.Type {
	case config.RemoteSFTP:
		port := r.Port
		if port == 0 {
			port = 22
		}
		addr := net.JoinHostPort(r.Host, strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			return fmt.Sprintf("Cannot reach %s:\n\n%v", addr, err), false
		}
		_ = conn.Close()
		return fmt.Sprintf("TCP connection to %s succeeded.\nAuthentication is verified on the first real backup run.", addr), true
	case config.RemoteS3:
		endpoint := r.Endpoint
		if endpoint == "" {
			return "The endpoint is empty.", false
		}
		if !strings.Contains(endpoint, "://") {
			endpoint = "https://" + endpoint
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(endpoint)
		if err != nil {
			return fmt.Sprintf("Cannot reach %s:\n\n%v", endpoint, err), false
		}
		_ = resp.Body.Close()
		return fmt.Sprintf("%s is reachable (HTTP %d).\nCredentials are verified on the first real backup run.", endpoint, resp.StatusCode), true
	case config.RemoteRclone:
		if _, err := exec.LookPath("rclone"); err != nil {
			return "The rclone binary was not found in PATH.\nInstall rclone and run `rclone config` first.", false
		}
		return fmt.Sprintf("rclone is installed. To verify the remote itself run:\n\n  rclone lsd %s", r.RcloneRemote), true
	default:
		return fmt.Sprintf("Unknown remote type %q.", r.Type), false
	}
}
