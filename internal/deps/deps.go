// Package deps ensures the helper binaries nest relies on (restic, rclone)
// are available, downloading them from the distribution mirror if needed.
package deps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/trusted-technologies/nest/internal/buildinfo"
	"github.com/trusted-technologies/nest/internal/config"
)

// downloadTimeout bounds a single binary download (restic/rclone are ~20 MB).
const downloadTimeout = 10 * time.Minute

// Ensure returns the path of the helper binary `name`. Lookup order:
//
//  1. $PATH (exec.LookPath)
//  2. <StateDir>/bin/<name>
//  3. download from {MirrorBaseURL}/bin/<name>-linux-<arch> into StateDir/bin
//
// When downloading, the sha256 of the file is verified against
// {MirrorBaseURL}/bin/sha256sums.txt if that file is reachable and lists the
// binary; a missing/unreachable sums file only logs a warning.
func Ensure(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	dir := filepath.Join(config.StateDir, "bin")
	local := filepath.Join(dir, name)
	if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
		return local, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	fileName := fmt.Sprintf("%s-linux-%s", name, buildinfo.ArchSuffix(runtime.GOARCH))
	url := buildinfo.MirrorBaseURL + "/bin/" + fileName
	log.Printf("downloading %s from %s", name, url)

	tmp, err := os.CreateTemp(dir, name+"-*.download")
	if err != nil {
		return "", fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	err = download(url, tmp)
	cerr := tmp.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	if err := verifySums(tmpPath, fileName); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, local); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("install %s: %w", local, err)
	}
	return local, nil
}

// download writes the body of url to w.
func download(url string, w io.Writer) error {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// verifySums checks the sha256 of path against the mirror's sha256sums.txt.
// An unreachable sums file is a warning, not an error.
func verifySums(path, fileName string) error {
	sumsURL := buildinfo.MirrorBaseURL + "/bin/sha256sums.txt"
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(sumsURL)
	if err != nil {
		log.Printf("warning: cannot fetch %s: %v (skipping checksum verification)", sumsURL, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("warning: %s returned %s (skipping checksum verification)", sumsURL, resp.Status)
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("warning: cannot read %s: %v (skipping checksum verification)", sumsURL, err)
		return nil
	}
	want, ok := parseSums(data)[fileName]
	if !ok {
		return nil // no entry for this file; nothing to verify against
	}
	got, err := sha256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s downloaded from %s: got %s, want %s",
			fileName, buildinfo.MirrorBaseURL+"/bin/"+fileName, got, want)
	}
	return nil
}

// parseSums parses sha256sums.txt style content: lines of "<hex>  <name>"
// (a "*" prefix on the name marks binary mode and is stripped).
func parseSums(data []byte) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		sums[name] = fields[0]
	}
	return sums
}

// sha256File returns the hex-encoded sha256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
