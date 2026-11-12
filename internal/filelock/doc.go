// Package filelock takes an exclusive lock on an open file, which is how
// copies of evado running at once take turns with a file they share: the
// session's rotating tokens, and the todos kept on this computer.
//
// The lock is advisory on Unix and covers the first byte on Windows, so
// the file locked is a lock file beside the data, never the data itself.
package filelock
