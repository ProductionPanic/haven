// Package vfstest provides an in-process SFTP-backed vfs.RemoteFS for tests.
package vfstest

import (
	"net"
	"runtime"
	"testing"

	"github.com/pkg/sftp"

	"github.com/ProductionPanic/haven/v2/internal/vfs"
)

// NewRemote returns a RemoteFS backed by an in-process SFTP server that
// serves the local file system. It is meant for tests in other packages.
//
// The server only understands POSIX paths. On Windows a test path such as
// C:\Users\... is resolved relative to the package directory, so tests
// would write into the source tree; they are skipped there instead.
func NewRemote(t testing.TB) *vfs.RemoteFS {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the in-process SFTP server needs POSIX paths")
	}
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
