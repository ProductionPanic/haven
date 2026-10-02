package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/pkg/sftp"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
)

// SFTPArgs returns the ssh argv (excluding "ssh") that starts the sftp
// subsystem on h. BatchMode keeps ssh from prompting inside the TUI; auth
// must come from keys or an agent (1Password's agent prompts in its own
// window, which is fine).
func SFTPArgs(h store.Host) ([]string, error) {
	opts, err := Options(h)
	if err != nil {
		return nil, err
	}
	args := append([]string{"-o", "BatchMode=yes", "-o", "ServerAliveInterval=15"}, opts...)
	return append(args, "-s", h.Target(), "sftp"), nil
}

// lockedBuffer collects ssh's stderr for error messages.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() > 8<<10 {
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

// DialSFTP opens an SFTP session to h through the system ssh binary, so it
// uses the same ~/.ssh/config, agent, keys and jump hosts as connecting.
func DialSFTP(ctx context.Context, h store.Host) (*vfs.RemoteFS, error) {
	args, err := SFTPArgs(h)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client, err := sftp.NewClientPipe(stdout, stdin,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
	if err != nil {
		stdin.Close()
		waitErr := cmd.Wait()
		return nil, dialError(h, err, waitErr, stderr.String())
	}
	closer := func() error {
		stdin.Close()
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil // ssh exits non-zero when the channel is torn down
		}
		return err
	}
	return vfs.NewRemoteFS(h.Name, client, closer), nil
}

func dialError(h store.Host, sftpErr, waitErr error, stderr string) error {
	msg := stderr
	if msg == "" && waitErr != nil {
		msg = waitErr.Error()
	}
	if msg == "" {
		msg = sftpErr.Error()
	}
	hint := ""
	switch {
	case strings.Contains(msg, "Permission denied"):
		hint = " (is your key loaded in the agent?)"
	case strings.Contains(msg, "Host key verification failed"):
		hint = fmt.Sprintf(` (connect once with "rootnet ssh %s" to accept the host key)`, h.Name)
	case strings.Contains(msg, "subsystem request failed"):
		hint = " (the server has SFTP disabled)"
	}
	return fmt.Errorf("sftp to %s: %s%s", h.Name, msg, hint)
}
