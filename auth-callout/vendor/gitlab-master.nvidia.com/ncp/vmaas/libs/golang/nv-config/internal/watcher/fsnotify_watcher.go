package watcher

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FSNotifyWatcher implements FileWatcher using fsnotify for OS-level events.
type FSNotifyWatcher struct {
	watcher   *fsnotify.Watcher
	callbacks map[string]func(string) error
	mutex     sync.RWMutex
	done      chan struct{}
}

// NewFSNotifyWatcher creates a new fsnotify-based file watcher.
func NewFSNotifyWatcher() (*FSNotifyWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	fw := &FSNotifyWatcher{
		watcher:   w,
		callbacks: make(map[string]func(string) error),
		done:      make(chan struct{}),
	}

	go fw.processEvents()
	return fw, nil
}

// WatchFile starts watching a file and calls the callback when it changes.
func (fw *FSNotifyWatcher) WatchFile(filePath string, callback func(string) error) error {
	cleanPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
	}

	fw.mutex.Lock()
	defer fw.mutex.Unlock()

	fw.callbacks[cleanPath] = callback
	if err := fw.watcher.Add(cleanPath); err != nil {
		delete(fw.callbacks, cleanPath)
		return fmt.Errorf("failed to watch file %s: %w", cleanPath, err)
	}

	return nil
}

// Stop stops watching all files and cleans up resources.
func (fw *FSNotifyWatcher) Stop() error {
	fw.mutex.Lock()
	defer fw.mutex.Unlock()

	select {
	case <-fw.done:
		return nil
	default:
		close(fw.done)
	}
	err := fw.watcher.Close()
	fw.callbacks = make(map[string]func(string) error)
	return err
}

func (fw *FSNotifyWatcher) processEvents() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				fw.handleFileChange(event.Name)
			}

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("File watcher error: %v\n", err)

		case <-fw.done:
			return
		}
	}
}

func (fw *FSNotifyWatcher) handleFileChange(filePath string) {
	cleanPath, err := filepath.Abs(filePath)
	if err != nil {
		return
	}

	fw.mutex.RLock()
	callback, exists := fw.callbacks[cleanPath]
	fw.mutex.RUnlock()

	if !exists {
		return
	}

	if err := callback(cleanPath); err != nil {
		fmt.Printf("File change callback error for %s: %v\n", cleanPath, err)
	}
}

// GetWatchedFiles returns the list of currently watched files.
func (fw *FSNotifyWatcher) GetWatchedFiles() []string {
	fw.mutex.RLock()
	defer fw.mutex.RUnlock()

	files := make([]string, 0, len(fw.callbacks))
	for filePath := range fw.callbacks {
		files = append(files, filePath)
	}
	return files
}
