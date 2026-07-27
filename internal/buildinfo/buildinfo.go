// Package buildinfo holds build-time constants shared by the CLI,
// the self-updater and the dependency downloader.
package buildinfo

// Version is injected at build time via:
//
//	-ldflags "-X github.com/trusted-technologies/nest/internal/buildinfo.Version=x.y.z"
var Version = "dev"

// MirrorBaseURL is the public base URL of this project on the distribution
// mirror (a Cloudflare R2 bucket behind a custom domain, shared with other
// projects — nest lives under the /nest prefix). It hosts the apt/yum
// repositories, standalone binaries and helper tools (restic, rclone).
const MirrorBaseURL = "https://mirror.trust-me.host/nest"

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
