// Package deps ensures the helper binaries nest relies on (restic, rclone)
// are available, downloading them from the distribution mirror if needed.
package deps

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/trusted-technologies/nest/internal/buildinfo"
	"github.com/trusted-technologies/nest/internal/config"
	"github.com/trusted-technologies/nest/internal/mirror"
)

// downloadTimeout bounds a single binary download (restic/rclone are ~20 MB).
const downloadTimeout = 10 * time.Minute

// Ensure returns the path of the helper binary `name`. Lookup order:
//
//  1. $PATH (exec.LookPath)
//  2. <StateDir>/bin/<name>
//  3. download from {MirrorBaseURL}/bin/<name>-linux-<arch> into StateDir/bin
//
// Downloads are verified against the mirror's sha256sums.txt detached-signed
// by the repository OpenPGP key (embedded in this binary). Verification is
// fail-closed: an unreachable/unsigned/mismatching sums file aborts the
// install — an unverified binary is never executed.
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

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	client := mirror.SecureClient(downloadTimeout)

	tmp, err := os.CreateTemp(dir, name+"-*.download")
	if err != nil {
		return "", fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	if err := mirror.DownloadBinary(ctx, client, url, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if err := mirror.VerifySignedSum(ctx, client, tmpPath, fileName); err != nil {
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
