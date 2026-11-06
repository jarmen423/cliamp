package remote

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjarneo/cliamp/internal/appdir"
)

// tokenFileName is the file in the config directory that holds the pairing
// token. Anyone with the token controls cliamp over the network.
const tokenFileName = "remote.token"

// tokenBytes is the entropy of a token. Its hex form has twice as many
// characters, which are all valid in a WebSocket subprotocol name.
const tokenBytes = 32

// TokenPath returns the path of the pairing token file.
func TokenPath() (string, error) {
	dir, err := appdir.Dir()
	if err != nil {
		return "", fmt.Errorf("remote: config dir: %w", err)
	}
	return filepath.Join(dir, tokenFileName), nil
}

// LoadToken returns the saved token. It returns an error that wraps
// fs.ErrNotExist when there is none.
func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("remote: read token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if !validToken(token) {
		return "", fmt.Errorf("remote: token in %s is malformed; run cliamp remote pair --rotate", path)
	}
	return token, nil
}

// LoadOrCreateToken returns the saved token, or saves and returns a new one
// when there is none.
func LoadOrCreateToken(path string) (string, error) {
	token, err := LoadToken(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return token, err
	}
	return RotateToken(path)
}

// RotateToken saves a new token in place of the old one and returns it.
// Devices that paired with the old token can no longer connect.
func RotateToken(path string) (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("remote: generate token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("remote: mkdir: %w", err)
	}
	// Write a temp file and rename it, so a running gateway never reads a
	// half-written token.
	tmp, err := os.CreateTemp(filepath.Dir(path), tokenFileName+".*")
	if err != nil {
		return "", fmt.Errorf("remote: write token: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("remote: write token: %w", err)
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return "", fmt.Errorf("remote: write token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("remote: write token: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("remote: write token: %w", err)
	}
	return token, nil
}

// validToken reports whether s has the form that RotateToken writes.
func validToken(s string) bool {
	if len(s) != tokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// tokenEqual compares a presented token with the saved one in constant time.
func tokenEqual(presented, saved string) bool {
	if saved == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(saved)) == 1
}
