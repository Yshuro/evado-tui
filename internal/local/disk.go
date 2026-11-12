package local

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nerdlabz/evado-tui/internal/filelock"
)

// The file can have more than one writer: a running TUI and a CLI command
// started next to it each hold a Backend on the same todos.json. Each keeps
// the whole state in memory and writes it all back, so without care one
// would save over the other's change.
//
// So every section that holds a backend's lock also holds a lock file
// beside the todos, which no other copy of evado can take meanwhile, and
// begins by reading the file again if anyone else wrote it since this
// backend last read or wrote it. Every change is saved before the section
// ends, so what is in memory outside a section is always what is on disk,
// and reading the file again never drops anything of this backend's own.

// diskWait bounds how long a backend waits for another copy of evado to
// finish with the file. Nothing holds it across a network call, so it is
// only ever held for as long as a save takes. Tests shorten it.
var diskWait = 10 * time.Second

// storeLock is the backend's lock: a mutex between the goroutines of this
// process, and the lock file between processes.
type storeLock struct {
	mu sync.Mutex
	b  *Backend
}

// Lock takes the lock and brings the state up to date with the file.
func (l *storeLock) Lock() {
	l.mu.Lock()
	if l.b != nil {
		l.b.enter()
	}
}

// Unlock lets the file and the state go.
func (l *storeLock) Unlock() {
	if l.b != nil {
		l.b.leave()
	}
	l.mu.Unlock()
}

// fileMark is what the file looked like when this backend last read or wrote
// it. A save replaces the file with a new one, so its identity changes as
// well as its size and time; gen is the count every copy of evado bumps
// in the lock file when it saves, which catches a save whose file looks
// the same to a filesystem with coarse times.
type fileMark struct {
	exists bool
	info   os.FileInfo
	gen    uint64
}

func (s fileMark) same(o fileMark) bool {
	if s.exists != o.exists || s.gen != o.gen {
		return false
	}
	if !s.exists {
		return true
	}
	return s.info.Size() == o.info.Size() && s.info.ModTime().Equal(o.info.ModTime()) && os.SameFile(s.info, o.info)
}

// enter takes the lock file and reads the todos again if they changed.
// Whatever stops that is kept in diskErr, which persist refuses to write
// over: the state in memory may then be older than the file. The caller
// holds mu.
func (b *Backend) enter() {
	b.diskErr = nil
	if b.path == "" {
		return
	}
	f, err := lockFile(b.path)
	if err != nil {
		b.diskErr = err
		return
	}
	b.held = f
	b.diskErr = b.refresh()
}

// leave lets the lock file go. The caller holds mu.
func (b *Backend) leave() {
	if b.held == nil {
		return
	}
	_ = filelock.Unlock(b.held)
	_ = b.held.Close()
	b.held = nil
}

// lockFile opens the lock file beside path and waits its turn for it. It
// is left in place afterwards: removing it would let two processes each
// hold a lock on a different file. It is opened for each section rather
// than kept, because Windows will not remove a directory holding a file
// that is open.
func lockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the lock beside %s: %w", path, err)
	}
	deadline := time.Now().Add(diskWait)
	for wait := time.Millisecond; ; wait = min(wait*2, 50*time.Millisecond) {
		ok, err := filelock.TryLock(f)
		if ok {
			return f, nil
		}
		if err == nil && time.Now().After(deadline) {
			err = errors.New("another evado has been using it for too long")
		}
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		time.Sleep(wait)
	}
}

// look reads what the file is like now. The caller holds the lock file.
func (b *Backend) look() (fileMark, error) {
	gen, err := readGen(b.held)
	if err != nil {
		return fileMark{}, err
	}
	fi, err := os.Stat(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return fileMark{gen: gen}, nil
	}
	if err != nil {
		return fileMark{}, err
	}
	// Windows reads a file's identity lazily, by name, the first time it is
	// compared. Comparing now pins the identity of the file as it is while
	// nobody else can replace it.
	os.SameFile(fi, fi)
	return fileMark{exists: true, info: fi, gen: gen}, nil
}

// refresh reads the file again if it is not as this backend left it. The
// caller holds the lock file.
func (b *Backend) refresh() error {
	now, err := b.look()
	if err != nil {
		return err
	}
	if b.loaded && now.same(b.seen) {
		return nil
	}
	st, err := load(b.path, b.now())
	if err != nil {
		return err
	}
	if b.loaded {
		b.reloads++
	}
	b.st, b.seen, b.loaded = st, now, true
	return nil
}

// written records that this backend wrote or removed the file, bumping the
// count in the lock file first so that a crash in between costs another
// copy of evado a needless read, never a missed one. The caller holds the
// lock file.
func (b *Backend) written(write func() error) error {
	if b.held == nil {
		return write()
	}
	gen := b.seen.gen + 1
	if err := writeGen(b.held, gen); err != nil {
		return err
	}
	b.seen.gen = gen
	err := write()
	if now, lerr := b.look(); lerr == nil {
		b.seen = now
	} else {
		// Not knowing what the file is like now, read it next time.
		b.loaded = false
	}
	return err
}

// readGen reads the save count from the lock file. An empty one, new or
// left by a version of evado that did not count, is zero.
func readGen(f *os.File) (uint64, error) {
	buf := make([]byte, 32)
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	s := strings.TrimSpace(string(buf[:n]))
	if s == "" {
		return 0, nil
	}
	gen, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, nil // something else wrote here; the next save repairs it
	}
	return gen, nil
}

func writeGen(f *os.File, gen uint64) error {
	s := strconv.FormatUint(gen, 10) + "\n"
	if _, err := f.WriteAt([]byte(s), 0); err != nil {
		return err
	}
	return f.Truncate(int64(len(s)))
}

// Reloads is how many times this backend has read the file again because
// another copy of evado changed it. It looks at the file first, so a
// running interface can poll it to notice changes made from the command
// line.
func (b *Backend) Reloads() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reloads
}
