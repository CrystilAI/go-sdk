package internal

import (
	"runtime/debug"
	"sync"
)

// BuildDeps lists the modules the program is built with, read once. Go records
// none in test binaries, so a test that needs them replaces this before it runs.
var BuildDeps = sync.OnceValue(func() []*debug.Module {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info.Deps
})
