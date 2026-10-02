package transfer

import (
	"context"
	"io"
	"io/fs"
)

type progressReader struct {
	ctx    context.Context
	r      io.Reader
	size   int64
	onRead func(int)
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.onRead(n)
	}
	return n, err
}

// Size lets *sftp.File.ReadFrom pick concurrent writes.
func (p *progressReader) Size() int64 { return p.size }

type progressWriter struct {
	ctx     context.Context
	w       io.Writer
	onWrite func(int)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	if n > 0 {
		p.onWrite(n)
	}
	return n, err
}

func fileMode(m uint32) fs.FileMode {
	if m == 0 {
		return 0o644
	}
	return fs.FileMode(m).Perm()
}
