// Package mirror fetches artifacts from the distribution mirror and
// verifies them cryptographically.
//
// Threat model: the mirror (a Cloudflare R2 bucket) is a single point of
// compromise — an attacker with write access to the bucket could serve
// malicious binaries to every nest installation, where they would run as
// root. TLS does not help there: the legitimate server itself would serve
// the malicious content. Therefore:
//
//   - sha256sums.txt is detached-signed with the repository OpenPGP key;
//     the public key is embedded in the nest binary (buildinfo.RepoPublicKey),
//     so trust never comes from the mirror itself;
//   - every verification failure is fatal (fail-closed) — a missing sums
//     file, a missing signature, an invalid signature or a hash mismatch
//     all refuse the download;
//   - redirects to non-HTTPS URLs or other hosts are rejected;
//   - downloaded binaries are sanity-checked (ELF magic, size limit) and
//     fsynced before the caller atomically renames them into place.
package mirror

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"

	"github.com/trusted-technologies/nest/internal/buildinfo"
)

// maxBinarySize bounds a binary download (restic/rclone/nest are ~20-50 MB).
const maxBinarySize = 300 << 20

// maxTextSize bounds metadata downloads (VERSION, sha256sums.txt, ...).
const maxTextSize = 1 << 20

// minELFSize is the smallest plausible static helper binary.
const minELFSize = 1 << 20

// SecureClient returns an HTTP client that refuses redirects to non-HTTPS
// URLs or to a different host (TLS-downgrade / open-redirect protection).
func SecureClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing redirect to non-HTTPS URL %s", req.URL)
			}
			if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("refusing redirect to a different host %s", req.URL.Host)
			}
			return nil
		},
	}
}

// FetchText GETs url and returns the trimmed body (max maxTextSize).
func FetchText(ctx context.Context, client *http.Client, url string) (string, error) {
	data, err := fetch(ctx, client, url, maxTextSize)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// FetchRaw GETs url and returns the raw body (max maxTextSize).
func FetchRaw(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	return fetch(ctx, client, url, maxTextSize)
}

func fetch(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

// DownloadBinary GETs url into path, enforcing a size limit and an
// ELF-magic sanity check, and fsyncs the file before returning (so a later
// atomic rename never commits a torn write).
func DownloadBinary(ctx context.Context, client *http.Client, url, path string) error {
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBinarySize+1))
	if err == nil && n > maxBinarySize {
		err = fmt.Errorf("download exceeds %d bytes", maxBinarySize)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return checkELF(path)
}

// checkELF verifies that path starts with the ELF magic and is plausibly
// sized — a first line of defense against HTML error pages or truncated
// origin responses that arrived with a 200 status.
func checkELF(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("%s: not an executable: %w", path, err)
	}
	if magic != [4]byte{0x7f, 'E', 'L', 'F'} {
		return fmt.Errorf("%s: not an ELF binary (bad magic)", path)
	}
	if fi, err := f.Stat(); err == nil && fi.Size() < minELFSize {
		return fmt.Errorf("%s: suspiciously small (%d bytes)", path, fi.Size())
	}
	return nil
}

// VerifySignedSum checks that the file at path is listed in the mirror's
// sha256sums.txt with a matching digest AND that sha256sums.txt carries a
// valid detached signature by the embedded repository key. Every failure
// mode is fatal: there is no "skip verification" path.
func VerifySignedSum(ctx context.Context, client *http.Client, path, fileName string) error {
	sumsURL := buildinfo.MirrorBaseURL + "/bin/sha256sums.txt"
	sums, err := FetchRaw(ctx, client, sumsURL)
	if err != nil {
		return fmt.Errorf("cannot fetch %s (refusing unverified download): %w", sumsURL, err)
	}
	sig, err := FetchRaw(ctx, client, sumsURL+".asc")
	if err != nil {
		return fmt.Errorf("cannot fetch %s.asc (refusing unsigned checksums): %w", sumsURL, err)
	}
	if err := verifySignature(sums, sig); err != nil {
		return fmt.Errorf("invalid signature on %s: %w", sumsURL, err)
	}
	want, ok := ParseSums(sums)[fileName]
	if !ok {
		return fmt.Errorf("%s: no entry for %s (refusing unverified download)", sumsURL, fileName)
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

// verifySignature checks a detached OpenPGP signature (armored or binary)
// over data against the embedded repository public key.
func verifySignature(data, sig []byte) error {
	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(buildinfo.RepoPublicKey))
	if err != nil {
		return fmt.Errorf("read embedded repository key: %w", err)
	}
	if bytes.Contains(sig, []byte("-----BEGIN PGP SIGNATURE-----")) {
		_, err = openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	} else {
		_, err = openpgp.CheckDetachedSignature(keyring, bytes.NewReader(data), bytes.NewReader(sig), nil)
	}
	return err
}

// ParseSums parses sha256sums.txt style content: lines of "<hex>  <name>"
// (a "*" prefix on the name marks binary mode and is stripped).
func ParseSums(data []byte) map[string]string {
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
