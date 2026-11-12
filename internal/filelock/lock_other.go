//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package filelock

import "os"

// TryLock has no file lock to take on this platform, so callers are
// serialized within one process only.
func TryLock(*os.File) (bool, error) { return true, nil }

// Unlock releases a lock TryLock took.
func Unlock(*os.File) error { return nil }
