// Package selfupdate replaces the running nest binary with the latest
// release from the distribution mirror.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/trusted-technologies/nest/internal/buildinfo"
)

// Run checks the mirror for a newer nest release and, if one exists,
// downloads it and atomically replaces the current executable.
func Run(ctx context.Context) error {
	current := strings.TrimPrefix(strings.TrimSpace(buildinfo.Version), "v")
	if current == "dev" || current == "" {
		fmt.Printf("development build (version %q) — self-update disabled\n", buildinfo.Version)
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Minute}

	latest, err := fetchText(ctx, client, buildinfo.MirrorBaseURL+"/VERSION")
	if err != nil {
		return fmt.Errorf("fetch %s/VERSION: %w", buildinfo.MirrorBaseURL, err)
	}
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	if latest == "" {
		return fmt.Errorf("mirror %s/VERSION returned an empty version", buildinfo.MirrorBaseURL)
	}
	if latest == current {
		fmt.Printf("nest %s is already up to date\n", current)
		return nil
	}
	fmt.Printf("updating nest %s -> %s\n", current, latest)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	name := "nest-linux-" + buildinfo.ArchSuffix(runtime.GOARCH)
	url := buildinfo.MirrorBaseURL + "/bin/" + name
	tmp := exe + ".new"
	if err := downloadFile(ctx, client, url, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := verify(ctx, client, tmp, name); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	fmt.Printf("nest updated to %s (%s)\n", latest, exe)
	return nil
}

// fetchText GETs url and returns the trimmed body.
func fetchText(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// downloadFile downloads url to path.
func downloadFile(ctx context.Context, client *http.Client, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

// verify checks the sha256 of path against the mirror's sha256sums.txt.
// An unreachable sums file or a missing entry is a warning, not an error.
func verify(ctx context.Context, client *http.Client, path, fileName string) error {
	sumsURL := buildinfo.MirrorBaseURL + "/bin/sha256sums.txt"
	sums, err := fetchText(ctx, client, sumsURL)
	if err != nil {
		log.Printf("warning: cannot fetch %s: %v (skipping checksum verification)", sumsURL, err)
		return nil
	}
	var want string
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == fileName {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", fileName, got, want)
	}
	return nil
}
