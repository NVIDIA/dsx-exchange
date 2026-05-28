package watcher

import (
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// fileState holds stored mtime and content hash for a watched file.
type fileState struct {
	ModTime time.Time
	Hash    [sha256.Size]byte
}

func fileStateFrom(path string) (fileState, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileState{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fileState{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fileState{}, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return fileState{ModTime: info.ModTime(), Hash: digest}, nil
}

// PollingWatcher implements FileWatcher by periodically stat-ing and hashing watched files.
type PollingWatcher struct {
	interval  time.Duration
	callbacks map[string]func(string) error
	lastState map[string]fileState
	mutex     sync.RWMutex
	done      chan struct{}
}

// NewPollingWatcher creates a watcher that checks file modification times at the given interval.
func NewPollingWatcher(interval time.Duration) *PollingWatcher {
	pw := &PollingWatcher{
		interval:  interval,
		callbacks: make(map[string]func(string) error),
		lastState: make(map[string]fileState),
		done:      make(chan struct{}),
	}
	go pw.poll()
	return pw
}

// WatchFile starts watching a file and calls the callback when its content changes (mtime or hash).
func (pw *PollingWatcher) WatchFile(filePath string, callback func(string) error) error {
	cleanPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
	}

	state, err := fileStateFrom(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cleanPath, err)
	}

	pw.mutex.Lock()
	defer pw.mutex.Unlock()
	pw.callbacks[cleanPath] = callback
	pw.lastState[cleanPath] = state
	return nil
}

// Stop stops the polling goroutine and clears state.
func (pw *PollingWatcher) Stop() error {
	pw.mutex.Lock()
	defer pw.mutex.Unlock()

	select {
	case <-pw.done:
		return nil
	default:
		close(pw.done)
	}
	pw.callbacks = make(map[string]func(string) error)
	pw.lastState = make(map[string]fileState)
	return nil
}

func (pw *PollingWatcher) poll() {
	ticker := time.NewTicker(pw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-pw.done:
			return
		case <-ticker.C:
			pw.checkFiles()
		}
	}
}

func (pw *PollingWatcher) checkFiles() {
	pw.mutex.RLock()
	files := slices.Collect(maps.Keys(pw.callbacks))
	pw.mutex.RUnlock()

	for _, cleanPath := range files {
		current, err := fileStateFrom(cleanPath)
		if err != nil {
			continue
		}

		pw.mutex.Lock()
		last := pw.lastState[cleanPath]
		callback := pw.callbacks[cleanPath]
		if current != last {
			pw.lastState[cleanPath] = current
			cb := callback
			pw.mutex.Unlock()
			if cb != nil {
				if err := cb(cleanPath); err != nil {
					fmt.Printf("Polling watcher callback error for %s: %v\n", cleanPath, err)
				}
			}
		} else {
			pw.mutex.Unlock()
		}
	}
}
