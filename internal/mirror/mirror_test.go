package mirror

import (
	"net/http"
	"testing"
)

func TestParseSums(t *testing.T) {
	data := []byte(`
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  restic-linux-amd64
5d41402abc4b2a76b9719d911017c592 *rclone-linux-arm64
not a valid line
too many fields here buddy

`)
	sums := ParseSums(data)
	if got := sums["restic-linux-amd64"]; got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("restic-linux-amd64: got %q", got)
	}
	if got := sums["rclone-linux-arm64"]; got != "5d41402abc4b2a76b9719d911017c592" {
		t.Errorf("rclone-linux-arm64 (binary marker): got %q", got)
	}
	if _, ok := sums["not"]; ok {
		t.Errorf("invalid line should be skipped, got %v", sums)
	}
	if len(sums) != 2 {
		t.Errorf("want 2 entries, got %d: %v", len(sums), sums)
	}
}

func TestSecureClientRejectsHTTPRedirect(t *testing.T) {
	c := SecureClient(0)
	err := c.CheckRedirect(newReq(t, "http://evil.example/x"), []*http.Request{newReq(t, "https://mirror.trust-me.host/x")})
	if err == nil {
		t.Fatal("http redirect must be rejected")
	}
	err = c.CheckRedirect(newReq(t, "https://other.example/x"), []*http.Request{newReq(t, "https://mirror.trust-me.host/x")})
	if err == nil {
		t.Fatal("cross-host redirect must be rejected")
	}
	err = c.CheckRedirect(newReq(t, "https://mirror.trust-me.host/y"), []*http.Request{newReq(t, "https://mirror.trust-me.host/x")})
	if err != nil {
		t.Fatalf("same-host https redirect must be allowed: %v", err)
	}
}

func newReq(t *testing.T, url string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
