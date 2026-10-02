// Package vfstest provides an in-process SFTP-backed vfs.RemoteFS for tests.
package vfstest

import (
	"net"
	"testing"

	"github.com/pkg/sftp"

	"github.com/ProductionPanic/rootnet-cli/internal/vfs"
)

// NewRemote returns a RemoteFS backed by an in-process SFTP server that
// serves the local file system. It is meant for tests in other packages.
func NewRemote(t testing.TB) *vfs.RemoteFS {
	t.Helper()
	cliConn, srvConn := net.Pipe()
	srv, err := sftp.NewServer(srvConn)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	client, err := sftp.NewClientPipe(cliConn, cliConn)
	if err != nil {
		t.Fatal(err)
	}
	r := vfs.NewRemoteFS("test", client, srv.Close)
	t.Cleanup(func() { r.Close() })
	return r
}
