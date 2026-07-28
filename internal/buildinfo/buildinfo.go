// Package buildinfo holds build-time constants shared by the CLI,
// the self-updater and the dependency downloader.
package buildinfo

import _ "embed"

// Version is injected at build time via:
//
//	-ldflags "-X github.com/trusted-technologies/nest/internal/buildinfo.Version=x.y.z"
var Version = "dev"

// MirrorBaseURL is the public base URL of this project on the distribution
// mirror (a Cloudflare R2 bucket behind a custom domain, shared with other
// projects — nest lives under the /nest prefix). It hosts the apt/yum
// repositories, standalone binaries and helper tools (restic, rclone).
const MirrorBaseURL = "https://mirror.trust-me.host/nest"

// RepoPublicKey is the ASCII-armored OpenPGP public key (Trusted
// Technologies Repo, fingerprint 53D158C2C57D928DF0F5990C1AF850157FCE9AD2)
// that signs sha256sums.txt on the mirror. It is embedded into the binary
// so downloaded helpers and updates can be verified against an
// uncompromisable-by-the-mirror trust anchor.
//
//go:embed trusted-technologies.asc
var RepoPublicKey []byte

// BinaryName maps the running GOARCH to the arch suffix used on the mirror.
func ArchSuffix(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "armhf"
	default:
		return goarch
	}
}
