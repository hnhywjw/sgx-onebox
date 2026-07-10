package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func sshConnect(host string, port int, user string, password []byte, knownHostKey string, timeout time.Duration) (*ssh.Client, error) {
	strictCheck := os.Getenv("SSH_STRICT_HOST_KEY_CHECKING") != "0"
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(string(password))},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint := base64.StdEncoding.EncodeToString(sha256Hash(key.Marshal()))
			if knownHostKey != "" {
				if knownHostKey != fingerprint {
					return fmt.Errorf("ssh host key mismatch for %s: expected %s, got %s", hostname, knownHostKey, fingerprint)
				}
				return nil
			}
			if strictCheck {
				return fmt.Errorf("ssh host key not configured for %s (fingerprint: %s)", hostname, fingerprint)
			}
			log.Printf("[executor] WARN: no known host key configured for %s, accepting fingerprint %s", hostname, fingerprint)
			return nil
		},
		Timeout: timeout,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	return client, nil
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func sshExec(client *ssh.Client, cmd string, timeout time.Duration) (string, error) {
	return sshExecWithInput(client, cmd, nil, timeout)
}

func sshExecWithInput(client *ssh.Client, cmd string, input io.Reader, timeout time.Duration) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if input != nil {
		session.Stdin = input
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		session.Close()
		return "", fmt.Errorf("ssh exec timeout after %v", timeout)
	case err := <-done:
		if err != nil {
			return strings.TrimSpace(stdout.String()), fmt.Errorf("ssh exec %q: %w (stderr: %s)", redactSensitiveCommand(cmd), err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(stdout.String()), nil
	}
}

func sshUploadFileBase64(client *ssh.Client, localPath string, remotePath string, timeout time.Duration) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	const maxUploadSize = 500 << 20
	if info.Size() > maxUploadSize {
		return "", fmt.Errorf("file size %d exceeds limit %d", info.Size(), maxUploadSize)
	}
	cleanPath := filepath.Clean(localPath)
	if cleanPath != localPath {
		return "", fmt.Errorf("invalid file path")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	reader, writer := io.Pipe()
	go func() {
		defer file.Close()
		encoder := base64.NewEncoder(base64.StdEncoding, writer)
		_, copyErr := io.Copy(encoder, file)
		closeErr := encoder.Close()
		if copyErr != nil {
			_ = writer.CloseWithError(copyErr)
			return
		}
		_ = writer.CloseWithError(closeErr)
	}()
	return sshExecWithInput(client, "base64 -d > "+shellQuote(remotePath), reader, timeout)
}

func redactSensitiveCommand(cmd string) string {
	result := cmd
	for _, key := range []string{"K3S_TOKEN="} {
		searchFrom := 0
		for {
			idx := strings.Index(result[searchFrom:], key)
			if idx < 0 {
				break
			}
			idx += searchFrom
			start := idx + len(key)
			end := start
			quote := byte(0)
			if end < len(result) && (result[end] == '\'' || result[end] == '"') {
				quote = result[end]
				end++
			}
			for end < len(result) {
				if quote != 0 {
					if result[end] == quote {
						end++
						break
					}
				} else {
					if result[end] == ' ' || result[end] == ';' {
						break
					}
				}
				end++
			}
			result = result[:start] + "***" + result[end:]
			searchFrom = start + 3
		}
	}
	return result
}
