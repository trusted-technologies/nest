package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// DefaultKeyPath returns the path of the nest-managed SSH keypair.
func DefaultKeyPath(sshDir string) string {
	return filepath.Join(sshDir, "id_ed25519")
}

// EnsureKeyPair makes sure an ed25519 keypair exists in sshDir and returns
// (privateKeyPath, publicKeyPath). The public key is what must be added to
// the backup server's authorized_keys.
func EnsureKeyPair(sshDir string) (string, string, error) {
	priv := DefaultKeyPath(sshDir)
	pub := priv + ".pub"
	if _, err := os.Stat(priv); err == nil {
		return priv, pub, nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return "", "", err
	}
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ssh key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", fmt.Errorf("encode ssh key: %w", err)
	}
	if err := os.WriteFile(priv, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", "", err
	}
	spub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(pub, ssh.MarshalAuthorizedKey(spub), 0o644); err != nil {
		return "", "", err
	}
	return priv, pub, nil
}

// PublicKey returns the contents of the nest public key (empty string if
// none exists yet).
func PublicKey(sshDir string) string {
	data, _ := os.ReadFile(DefaultKeyPath(sshDir) + ".pub")
	return string(data)
}
