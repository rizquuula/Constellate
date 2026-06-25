// Package scrollbackfile implements session.ScrollbackArchive using one file
// per session stored in a configurable directory.
//
// Files are named <sessionID>.scrollback. Writes are atomic (temp file + rename).
// A total-size cap evicts the oldest archives (by mtime) when exceeded.
package scrollbackfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultDiskCap = 64 * 1024 * 1024 // 64 MiB

// Archive persists per-session scrollback as individual files in dir.
type Archive struct {
	dir    string
	capB   int64 // total-size cap in bytes; 0 means use defaultDiskCap
}

// New returns an Archive that stores scrollback files in dir. capBytes sets the
// total-size cap; <= 0 uses the default 64 MiB. dir is created on first Save.
func New(dir string, capBytes int64) *Archive {
	if capBytes <= 0 {
		capBytes = defaultDiskCap
	}
	return &Archive{dir: dir, capB: capBytes}
}

// Save atomically persists data for sessionID. It writes to a temp file in the
// same directory, then renames it into place. After writing it enforces the
// disk cap by removing the oldest archives.
func (a *Archive) Save(sessionID string, data []byte) error {
	if err := validateID(sessionID); err != nil {
		return err
	}
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return fmt.Errorf("scrollbackfile: mkdir %s: %w", a.dir, err)
	}

	target := a.path(sessionID)

	// Write to a temp file in the same directory so rename is atomic.
	f, err := os.CreateTemp(a.dir, ".sb-tmp-*")
	if err != nil {
		return fmt.Errorf("scrollbackfile: create temp: %w", err)
	}
	tmpPath := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("scrollbackfile: write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("scrollbackfile: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("scrollbackfile: rename to %s: %w", target, err)
	}

	a.enforceCap(sessionID)
	return nil
}

// Load retrieves previously saved bytes for sessionID. Returns (nil, nil) when
// no archive exists for that session.
func (a *Archive) Load(sessionID string) ([]byte, error) {
	if err := validateID(sessionID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(a.path(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scrollbackfile: load %s: %w", sessionID, err)
	}
	return data, nil
}

// Delete removes the archive for sessionID. It is a no-op when no archive exists.
func (a *Archive) Delete(sessionID string) error {
	if err := validateID(sessionID); err != nil {
		return err
	}
	err := os.Remove(a.path(sessionID))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("scrollbackfile: delete %s: %w", sessionID, err)
	}
	return nil
}

// path returns the filesystem path for a session's archive file.
func (a *Archive) path(sessionID string) string {
	return filepath.Join(a.dir, sessionID+".scrollback")
}

// validateID rejects session IDs that contain path separators or are otherwise
// unsafe for use as filenames. Session IDs are ULIDs/UUIDs so this is defensive.
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("scrollbackfile: sessionID is empty")
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("scrollbackfile: sessionID %q contains path separator", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("scrollbackfile: sessionID %q contains '..'", id)
	}
	return nil
}

// archiveEntry holds the metadata needed for LRU eviction.
type archiveEntry struct {
	path  string
	size  int64
	mtime time.Time
}

// enforceCap removes the oldest archives when the total size of all .scrollback
// files in the directory exceeds the configured cap. The file for justWritten is
// never deleted.
func (a *Archive) enforceCap(justWritten string) {
	entries, total := a.scanDir()
	if total <= a.capB {
		return
	}

	// Sort oldest-first so we delete least-recently-used archives first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mtime.Before(entries[j].mtime)
	})

	protected := a.path(justWritten)
	for _, e := range entries {
		if total <= a.capB {
			break
		}
		if e.path == protected {
			continue
		}
		if err := os.Remove(e.path); err == nil {
			total -= e.size
		}
	}
}

// scanDir reads all .scrollback files in the archive dir and returns their info
// and the total size.
func (a *Archive) scanDir() ([]archiveEntry, int64) {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil, 0
	}

	var out []archiveEntry
	var total int64
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".scrollback") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		// Skip temp files left from a crash.
		if strings.HasPrefix(de.Name(), ".sb-tmp-") {
			_ = os.Remove(filepath.Join(a.dir, de.Name()))
			continue
		}
		out = append(out, archiveEntry{
			path:  filepath.Join(a.dir, de.Name()),
			size:  info.Size(),
			mtime: modTime(info),
		})
		total += info.Size()
	}
	return out, total
}

// modTime returns the modification time of fi, falling back to zero time.
func modTime(fi fs.FileInfo) time.Time {
	return fi.ModTime()
}
