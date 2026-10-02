// Package transfer copies files between vfs file systems with a worker
// queue, progress reporting, conflict handling and cancellation.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
)

// PartSuffix is appended to files while they are being written. They are
// renamed into place only once complete, so a cancelled upload never leaves
// a half-written file under the real name.
const PartSuffix = ".rootnet-part"

// Policy decides what happens when the destination already exists.
type Policy int

const (
	Ask Policy = iota
	Overwrite
	Skip
	OverwriteIfNewer
	RenameNew // keep both: write "name (1).ext"
)

func (p Policy) String() string {
	return [...]string{"ask", "overwrite", "skip", "overwrite if newer", "rename"}[p]
}

// Decision answers a conflict question.
type Decision struct {
	Policy Policy // never Ask
	ForAll bool   // apply to the rest of the batch
}

// State of a file job.
type State int

const (
	Queued State = iota
	Running
	Done
	Skipped
	Failed
	Cancelled
)

func (s State) String() string {
	return [...]string{"queued", "running", "done", "skipped", "failed", "cancelled"}[s]
}

// Request copies Sources (paths on Src) into the directory DstDir on Dst.
type Request struct {
	Src     vfs.FS
	Sources []string
	Dst     vfs.FS
	DstDir  string
	DstName string // optional new name; only valid with a single source
	Policy  Policy // Ask prompts through Conflict events
}

// File is one file copy within a batch.
type File struct {
	SrcPath, DstPath string
	Size             int64
	ModTime          time.Time
	Mode             uint32
	IsDir            bool // an empty directory to create

	State State
	Err   error
	Done  int64
}

// Event is sent on Engine.Events.
type Event interface{ isEvent() }

// Progress reports the state of a batch. It is throttled to ~10/s while
// bytes flow, plus one event per file state change.
type Progress struct {
	Batch          int
	Label          string
	Total, Done    int64 // bytes
	Files, Settled int   // file counts (settled = done+skipped+failed)
	Skipped        int
	Failed         int
	Current        string // file being copied
	Rate           float64
	Finished       bool
	Cancelled      bool
	Err            error // first error, if any
}

// Conflict asks the UI what to do about an existing destination. Exactly
// one Decision must be sent on Reply. Questions within a batch are asked
// one at a time.
type Conflict struct {
	Batch    int
	File     File
	Existing vfs.Entry
	Reply    chan<- Decision
}

func (Progress) isEvent() {}
func (Conflict) isEvent() {}

// Engine runs transfer batches. Create it with New and read Events.
type Engine struct {
	events  chan Event
	workers int
	nextID  atomic.Int64

	mu      sync.Mutex
	cancels map[int]context.CancelFunc
}

// New creates an engine running up to workers file copies at once.
func New(workers int) *Engine {
	return &Engine{
		events:  make(chan Event, 64),
		workers: max(1, workers),
		cancels: map[int]context.CancelFunc{},
	}
}

// Events delivers progress and conflict events. It must be drained.
func (e *Engine) Events() <-chan Event { return e.events }

// Cancel stops a batch; running files are aborted and their partial files
// removed.
func (e *Engine) Cancel(batch int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.cancels[batch]; ok {
		c()
	}
}

// CancelAll stops every running batch.
func (e *Engine) CancelAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.cancels {
		c()
	}
}

// Start begins a batch in the background and returns its ID.
func (e *Engine) Start(ctx context.Context, req Request) int {
	id := int(e.nextID.Add(1))
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancels[id] = cancel
	e.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			e.mu.Lock()
			delete(e.cancels, id)
			e.mu.Unlock()
		}()
		e.run(ctx, id, req)
	}()
	return id
}

// Run executes a batch synchronously. Events must still be drained (the
// final Progress is both sent and returned).
func (e *Engine) Run(ctx context.Context, req Request) Progress {
	id := int(e.nextID.Add(1))
	return e.run(ctx, id, req)
}

type batch struct {
	id    int
	req   Request
	label string

	askMu sync.Mutex // one conflict question at a time

	mu      sync.Mutex
	policy  Policy
	files   []*File
	total   int64
	done    atomic.Int64
	current string
	err     error
	start   time.Time // first byte copied

	// moving-average rate for the live display
	rate     float64
	rateAt   time.Time
	rateDone int64

	lastEmit time.Time
}

func (e *Engine) run(ctx context.Context, id int, req Request) Progress {
	b := &batch{id: id, req: req, policy: req.Policy}
	b.label = label(req)

	files, err := expand(ctx, req)
	if err != nil {
		b.err = err
	}
	b.files = files
	for _, f := range files {
		b.total += f.Size
	}
	e.emit(b, true)

	jobs := make(chan *File)
	var wg sync.WaitGroup
	for w := 0; w < e.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				e.copyFile(ctx, b, f)
			}
		}()
	}
	for _, f := range files {
		if ctx.Err() != nil {
			f.State = Cancelled
			continue
		}
		select {
		case jobs <- f:
		case <-ctx.Done():
			f.State = Cancelled
		}
	}
	close(jobs)
	wg.Wait()

	p := b.progress(true)
	p.Cancelled = ctx.Err() != nil
	e.events <- p
	return p
}

func label(req Request) string {
	what := fmt.Sprintf("%d items", len(req.Sources))
	if len(req.Sources) == 1 {
		what = req.Src.Base(req.Sources[0])
	}
	arrow := "→"
	if req.Src.Remote() && !req.Dst.Remote() {
		arrow = "↓"
	} else if !req.Src.Remote() && req.Dst.Remote() {
		arrow = "↑"
	}
	return fmt.Sprintf("%s %s → %s", arrow, what, req.DstDir)
}

func (b *batch) progress(finished bool) Progress {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := Progress{
		Batch: b.id, Label: b.label, Total: b.total, Done: b.done.Load(),
		Files: len(b.files), Current: b.current, Finished: finished, Err: b.err,
	}
	for _, f := range b.files {
		switch f.State {
		case Done:
			p.Settled++
		case Skipped:
			p.Settled++
			p.Skipped++
		case Failed:
			p.Settled++
			p.Failed++
		}
	}
	now := time.Now()
	switch {
	case b.start.IsZero():
	case finished:
		if el := now.Sub(b.start).Seconds(); el > 0 {
			p.Rate = float64(p.Done) / el
		}
	default:
		if dt := now.Sub(b.rateAt).Seconds(); dt >= 0.25 {
			inst := float64(p.Done-b.rateDone) / dt
			if b.rate == 0 {
				b.rate = inst
			} else {
				b.rate = 0.7*b.rate + 0.3*inst
			}
			b.rateAt, b.rateDone = now, p.Done
		}
		p.Rate = b.rate
	}
	return p
}

// emit sends progress, throttled unless force.
func (e *Engine) emit(b *batch, force bool) {
	b.mu.Lock()
	now := time.Now()
	if !force && now.Sub(b.lastEmit) < 100*time.Millisecond {
		b.mu.Unlock()
		return
	}
	b.lastEmit = now
	b.mu.Unlock()
	p := b.progress(false)
	if force {
		e.events <- p
		return
	}
	select { // never block a copy on a slow UI
	case e.events <- p:
	default:
	}
}

// expand walks the sources and returns one File per regular file, creating
// nothing yet. Directories become their files under DstDir/<dirname>/...
func expand(ctx context.Context, req Request) ([]*File, error) {
	var files []*File
	var walk func(src, dst string) error
	walk = func(src, dst string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		e, err := req.Src.Stat(src)
		if err != nil {
			return err
		}
		if e.Broken {
			return nil
		}
		if !e.IsDir {
			files = append(files, &File{SrcPath: src, DstPath: dst, Size: e.Size, ModTime: e.ModTime, Mode: uint32(e.Mode.Perm())})
			return nil
		}
		if e.Link != "" {
			return nil // don't follow directory symlinks (avoids cycles)
		}
		children, err := req.Src.ReadDir(src)
		if err != nil {
			return err
		}
		vfs.Sort(children, vfs.SortName)
		if len(children) == 0 {
			// Represent empty directories so they're still created.
			files = append(files, &File{SrcPath: src, DstPath: dst, IsDir: true})
		}
		for _, c := range children {
			if err := walk(req.Src.Join(src, c.Name), req.Dst.Join(dst, c.Name)); err != nil {
				return err
			}
		}
		return nil
	}
	var errs []error
	for _, s := range req.Sources {
		name := req.Src.Base(s)
		if req.DstName != "" && len(req.Sources) == 1 {
			name = req.DstName
		}
		if err := walk(s, req.Dst.Join(req.DstDir, name)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s, err))
		}
	}
	return files, errors.Join(errs...)
}

func (e *Engine) setState(b *batch, f *File, s State, err error) {
	b.mu.Lock()
	f.State, f.Err = s, err
	if err != nil && b.err == nil && s == Failed {
		b.err = fmt.Errorf("%s: %w", f.SrcPath, err)
	}
	b.mu.Unlock()
	e.emit(b, true)
}

func (e *Engine) copyFile(ctx context.Context, b *batch, f *File) {
	if ctx.Err() != nil {
		e.setState(b, f, Cancelled, nil)
		return
	}
	dst := b.req.Dst
	if f.IsDir {
		if err := dst.MkdirAll(f.DstPath); err != nil {
			e.setState(b, f, Failed, err)
		} else {
			e.setState(b, f, Done, nil)
		}
		return
	}

	target, skip, err := e.resolveConflict(ctx, b, f)
	if err != nil {
		e.setState(b, f, stateFor(ctx, err), err)
		return
	}
	if skip {
		b.mu.Lock()
		b.total -= f.Size // keep the percentage meaningful
		b.mu.Unlock()
		e.setState(b, f, Skipped, nil)
		return
	}
	f.DstPath = target

	b.mu.Lock()
	f.State = Running
	b.current = b.req.Src.Base(f.SrcPath)
	b.mu.Unlock()

	if err := dst.MkdirAll(dst.Dir(target)); err != nil {
		e.setState(b, f, Failed, err)
		return
	}
	n, err := e.copyData(ctx, b, f, target)
	if err != nil {
		b.done.Add(-n) // don't count bytes of a failed file
		e.setState(b, f, stateFor(ctx, err), err)
		return
	}
	e.setState(b, f, Done, nil)
}

func stateFor(ctx context.Context, err error) State {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return Cancelled
	}
	return Failed
}

func (e *Engine) copyData(ctx context.Context, b *batch, f *File, target string) (int64, error) {
	src, dst := b.req.Src, b.req.Dst
	part := target + PartSuffix

	r, err := src.Open(f.SrcPath)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	w, err := dst.Create(part)
	if err != nil {
		return 0, err
	}

	counter := func(n int) {
		b.mu.Lock()
		if b.start.IsZero() {
			b.start, b.rateAt = time.Now(), time.Now()
		}
		b.mu.Unlock()
		b.done.Add(int64(n))
		e.emit(b, false)
	}
	var n int64
	if dst.Remote() {
		// Wrap the reader so *sftp.File.ReadFrom still sees the size and
		// writes concurrently.
		n, err = io.Copy(w, &progressReader{ctx: ctx, r: r, size: f.Size, onRead: counter})
	} else {
		n, err = io.Copy(&progressWriter{ctx: ctx, w: w, onWrite: counter}, r)
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = dst.Remove(part)
		return n, err
	}
	_ = dst.Chmod(part, fileMode(f.Mode))
	_ = dst.Chtimes(part, f.ModTime)
	if err := dst.Rename(part, target); err != nil {
		_ = dst.Remove(part)
		return n, err
	}
	return n, nil
}

func (e *Engine) resolveConflict(ctx context.Context, b *batch, f *File) (target string, skip bool, err error) {
	dst := b.req.Dst
	existing, err := dst.Stat(f.DstPath)
	if err != nil {
		return f.DstPath, false, nil // assume it doesn't exist
	}

	b.askMu.Lock()
	defer b.askMu.Unlock()
	b.mu.Lock()
	policy := b.policy // re-read: an earlier answer may have been "for all"
	b.mu.Unlock()

	if policy == Ask {
		reply := make(chan Decision, 1)
		select {
		case e.events <- Conflict{Batch: b.id, File: *f, Existing: existing, Reply: reply}:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
		var d Decision
		select {
		case d = <-reply:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
		policy = d.Policy
		if d.ForAll {
			b.mu.Lock()
			b.policy = d.Policy
			b.mu.Unlock()
		}
	}

	if existing.IsDir {
		return "", false, fmt.Errorf("%s is a directory", f.DstPath)
	}
	switch policy {
	case Skip:
		return "", true, nil
	case OverwriteIfNewer:
		if !f.ModTime.After(existing.ModTime) {
			return "", true, nil
		}
		return f.DstPath, false, nil
	case RenameNew:
		return freeName(dst, f.DstPath), false, nil
	default:
		return f.DstPath, false, nil
	}
}

// freeName returns "name (n).ext" for the first n that doesn't exist.
func freeName(fsys vfs.FS, p string) string {
	dir, base := fsys.Dir(p), fsys.Base(p)
	stem, ext := base, ""
	for i := len(base) - 1; i > 0; i-- {
		if base[i] == '.' {
			stem, ext = base[:i], base[i:]
			break
		}
	}
	for n := 1; ; n++ {
		cand := fsys.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if _, err := fsys.Stat(cand); err != nil {
			return cand
		}
	}
}
