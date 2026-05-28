package watcher

// FileWatcher watches files and invokes a callback when they change.
type FileWatcher interface {
	WatchFile(filePath string, callback func(string) error) error
	Stop() error
}
