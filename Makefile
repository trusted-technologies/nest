# nest — build, test, package and release helpers.
#
# Toolchain: `go` is taken from PATH, falling back to the repo-local
# .tools/go/bin/go. Override with `make GO=/path/to/go`.

GO ?= $(shell command -v go || echo $(CURDIR)/.tools/go/bin/go)
export GO

.DEFAULT_GOAL := build

.PHONY: build test vet package repo tools publish test-e2e test-backup clean

# Build static binaries for linux/amd64, arm64 and armhf into dist/bin/.
build:
	./scripts/build.sh

# Run the Go test suite.
test:
	$(GO) test ./...

# Run go vet.
vet:
	$(GO) vet ./...

# Build .deb/.rpm packages into dist/packages/ (requires nfpm).
package:
	./scripts/package.sh

# Build the apt and yum repositories into dist/repo/ (requires NEST_GPG_KEY).
repo:
	./scripts/make-apt-repo.sh
	./scripts/make-yum-repo.sh

# Download restic/rclone helper binaries into dist/tools/bin/.
tools:
	./scripts/fetch-tools.sh

# Upload dist/ artifacts to the Cloudflare R2 mirror
# (requires R2_ENDPOINT, R2_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY).
publish:
	./scripts/publish.sh

# End-to-end install test across distros in docker (requires docker).
test-e2e:
	./scripts/test-e2e.sh

# Functional backup/restore test against a local MinIO in docker.
test-backup:
	./scripts/test-backup.sh

# Remove all build artifacts.
clean:
	rm -rf dist
