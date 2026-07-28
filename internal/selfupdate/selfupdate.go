// Package selfupdate replaces the running nest binary with the latest
// release from the distribution mirror.
//
// The downloaded binary is verified against sha256sums.txt detached-signed
// by the repository OpenPGP key (the public key is embedded in the binary),
// and downgrades are refused: a mirror that serves an older (or replayed)
// VERSION must never move an installation backwards.
package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/trusted-technologies/nest/internal/buildinfo"
	"github.com/trusted-technologies/nest/internal/mirror"
)

// Run checks the mirror for a newer nest release and, if one exists,
// downloads it and atomically replaces the current executable.
func Run(ctx context.Context) error {
	current := strings.TrimPrefix(strings.TrimSpace(buildinfo.Version), "v")
	if current == "dev" || current == "" {
		fmt.Printf("development build (version %q) — self-update disabled\n", buildinfo.Version)
		return nil
	}
	client := mirror.SecureClient(5 * time.Minute)

	latest, err := mirror.FetchText(ctx, client, buildinfo.MirrorBaseURL+"/VERSION")
	if err != nil {
		return fmt.Errorf("fetch %s/VERSION: %w", buildinfo.MirrorBaseURL, err)
	}
	latest = strings.TrimPrefix(strings.TrimSpace(latest), "v")
	if latest == "" {
		return fmt.Errorf("mirror %s/VERSION returned an empty version", buildinfo.MirrorBaseURL)
	}
	switch cmp, verr := CompareVersions(latest, current); {
	case verr != nil:
		return fmt.Errorf("cannot compare versions (local %q, mirror %q): %w", current, latest, verr)
	case cmp == 0:
		fmt.Printf("nest %s is already up to date\n", current)
		return nil
	case cmp < 0:
		return fmt.Errorf("refusing to downgrade: mirror offers %s, installed is %s", latest, current)
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
	if err := mirror.DownloadBinary(ctx, client, url, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := mirror.VerifySignedSum(ctx, client, tmp, name); err != nil {
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

// CompareVersions compares two dotted-numeric versions ("1.0.4" vs
// "1.0.10"), returning -1, 0 or +1. Trailing pre-release/build suffixes
// ("-rc1", "+build") are stripped; a leading "v" is tolerated.
func CompareVersions(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	return 0, nil
}

// parseVersion splits a version string into its numeric components.
func parseVersion(v string) ([]int, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, fmt.Errorf("empty version")
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid version component %q in %q", part, v)
		}
		out = append(out, n)
	}
	return out, nil
}
