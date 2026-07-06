package substrate

import (
	"sync"

	"github.com/gofrs/flock"
)

// Advisory locks coordinate independent substrate processes; the local mutex
// also serializes goroutines because some flock implementations are scoped to
// a process rather than an individual file descriptor.
var localLocks sync.Map

func withFileLock(path string, fn func() error) error {
	value, _ := localLocks.LoadOrStore(path, &sync.Mutex{})
	local := value.(*sync.Mutex)
	local.Lock()
	defer local.Unlock()

	fileLock := flock.New(path)
	if err := fileLock.Lock(); err != nil {
		return err
	}
	defer func() { _ = fileLock.Unlock() }()
	return fn()
}
