package engine

import "io"

// defaultWrite is the process output hook used when a Session has no explicit
// Output. The app and tests install a closure so swapping os.Stdout still
// affects writes.
var defaultWrite func([]byte) (int, error)

// SetDefaultOutput installs the fallback terminal writer for browsers without
// an explicit Output.
func SetDefaultOutput(fn func([]byte) (int, error)) { defaultWrite = fn }

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }

func (b *Session) writer() io.Writer {
	if b.Output != nil {
		return b.Output
	}
	if defaultWrite != nil {
		return writeFunc(defaultWrite)
	}
	return io.Discard
}
