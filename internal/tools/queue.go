package tools

import (
	"os"
	"path/filepath"
	"sync"
)

// Serialize file mutation operations targeting the same file.
// Operations for different files still run in parallel.

var (
	queueMu    sync.Mutex
	fileQueues = map[string]chan struct{}{}
)

func getMutationQueueKey(filePath string) (string, error) {
	resolvedPath := filepath.Clean(filePath)
	real, err := filepath.EvalSymlinks(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return resolvedPath, nil
		}
		return "", err
	}
	return real, nil
}

func withFileMutationQueue(filePath string, fn func() error) error {
	key, err := getMutationQueueKey(filePath)
	if err != nil {
		return err
	}

	queueMu.Lock()
	prev := fileQueues[key]
	next := make(chan struct{})
	fileQueues[key] = next
	queueMu.Unlock()

	if prev != nil {
		<-prev
	}

	err = fn()

	close(next)

	queueMu.Lock()
	if fileQueues[key] == next {
		delete(fileQueues, key)
	}
	queueMu.Unlock()
	return err
}
