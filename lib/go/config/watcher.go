// Package config — fsnotify-based hot-reload watcher.
//
// NewWatcher parses the given YAML config file, starts a background goroutine
// listening for fs events, and atomically swaps the config pointer when the
// file changes. All middleware reads Current() — a wait-free atomic load.
package config

import (
	"fmt"
	"log"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a config file for changes and hot-swaps the parsed config.
type Watcher struct {
	path    string
	current atomic.Pointer[RadixIpConfig]
	fswt    *fsnotify.Watcher
	done    chan struct{}
}

// NewWatcher creates a Watcher for the given file path and starts the
// background fs-event goroutine. Call Stop() to release resources.
func NewWatcher(path string) (*Watcher, error) {
	// Parse initial config.
	cfg, err := LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("config watcher: initial load: %w", err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("config watcher: fsnotify: %w", err)
	}
	if err := fsw.Add(path); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("config watcher: watch %s: %w", path, err)
	}

	w := &Watcher{
		path: path,
		fswt: fsw,
		done: make(chan struct{}),
	}
	w.current.Store(cfg)

	go w.loop()
	return w, nil
}

// Current returns the most recently loaded config. This is a wait-free atomic
// load — safe to call on every request with zero lock contention.
func (w *Watcher) Current() *RadixIpConfig {
	return w.current.Load()
}

// Stop shuts down the background watcher goroutine and releases file handles.
func (w *Watcher) Stop() {
	_ = w.fswt.Close()
	<-w.done
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case event, ok := <-w.fswt.Events:
			if !ok {
				return // watcher closed
			}
			// React to writes and renames (editors like vim replace files).
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				w.reload()
				// Re-add the watch after rename (some editors replace the inode).
				if event.Has(fsnotify.Rename) {
					_ = w.fswt.Add(w.path)
				}
			}
		case err, ok := <-w.fswt.Errors:
			if !ok {
				return
			}
			log.Printf("radixip config watcher: fsnotify error: %v", err)
		}
	}
}

func (w *Watcher) reload() {
	cfg, err := LoadFromFile(w.path)
	if err != nil {
		log.Printf("radixip config watcher: reload failed (keeping old config): %v", err)
		return
	}
	w.current.Store(cfg)
	log.Printf("radixip config watcher: hot-reloaded %s ✓", w.path)
}
