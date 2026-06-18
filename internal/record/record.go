// Package record optionally tees proxied media segments to disk so that
// everything watched can be re-assembled later (e.g. with ffmpeg). Recording is a
// passive side-effect of streaming: segments are written as individual files,
// grouped per channel with chronologically sortable names, and the oldest files
// are deleted once the configured size cap is exceeded.
package record

import (
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// seenCap bounds the dedup set so it cannot grow without limit. Live manifests
// re-list recent segments on every refresh; this only needs to remember enough
// recent URLs to skip those duplicates.
const seenCap = 8192

// Recorder writes proxied media to disk under dir, keeping total size under maxBytes.
type Recorder struct {
	dir      string
	maxBytes int64

	mu    sync.Mutex
	seen  map[string]struct{} // recently recorded segment URLs (dedup)
	seq   uint64              // monotonic counter to break same-millisecond ties
	total int64               // current total bytes on disk
}

// New creates a Recorder writing under dir, capped at maxGB gigabytes. The caller
// is responsible for only enabling recording when maxGB > 0.
func New(dir string, maxGB float64) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Recorder{
		dir:      dir,
		maxBytes: int64(maxGB * (1 << 30)),
		seen:     make(map[string]struct{}, seenCap),
	}
	r.total = r.sizeOnDisk()
	return r, nil
}

// Entry is an open recording file. Write segment bytes to it, then Close it.
type Entry struct {
	f   *os.File
	rec *Recorder
}

func (e *Entry) Write(p []byte) (int, error) { return e.f.Write(p) }

// Close flushes the file and updates size accounting, evicting old files if the
// cap is now exceeded.
func (e *Entry) Close() error {
	err := e.f.Close()
	e.rec.account(e.f.Name())
	return err
}

// Start opens a recording file for a media response, or returns nil when the
// response is not media or its URL was already recorded.
func (r *Recorder) Start(contentType, rawURL string) *Entry {
	if !isMedia(contentType, rawURL) {
		return nil
	}
	r.mu.Lock()
	if _, dup := r.seen[rawURL]; dup {
		r.mu.Unlock()
		return nil
	}
	if len(r.seen) >= seenCap {
		r.seen = make(map[string]struct{}, seenCap)
	}
	r.seen[rawURL] = struct{}{}
	seq := r.seq
	r.seq++
	r.mu.Unlock()

	dir := filepath.Join(r.dir, channelKey(rawURL))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	name := fmt.Sprintf("%013d_%09d%s", time.Now().UnixMilli(), seq, segExt(rawURL))
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	return &Entry{f: f, rec: r}
}

func (r *Recorder) account(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.total += fi.Size()
	over := r.total > r.maxBytes
	r.mu.Unlock()
	if over {
		r.evict()
	}
}

// evict deletes the oldest files (by mtime) until total size is back under the cap.
// It recomputes the total from disk so accounting stays correct even across restarts.
func (r *Recorder) evict() {
	r.mu.Lock()
	defer r.mu.Unlock()

	type fileEntry struct {
		path string
		size int64
		mod  time.Time
	}
	var files []fileEntry
	var total int64
	filepath.WalkDir(r.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileEntry{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= r.maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
	r.total = total
}

func (r *Recorder) sizeOnDisk() int64 {
	var total int64
	filepath.WalkDir(r.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// isMedia reports whether a response should be recorded: audio/video content types
// (or a media file extension) — so manifests, images and EPG are skipped.
func isMedia(contentType, rawURL string) bool {
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/") {
		return true
	}
	if strings.Contains(ct, "mp2t") || strings.Contains(ct, "mpegts") {
		return true
	}
	switch strings.ToLower(segExt(rawURL)) {
	case ".ts", ".aac", ".mp4", ".m4s", ".m4a", ".mpg", ".mpeg":
		return true
	}
	return false
}

// channelKey derives a per-channel directory name from the upstream URL. For
// hls.gd-style URLs (/ch963/...) it is the first path segment; for Xtream stream
// paths (/{type}/{user}/{pass}/{id}.ext) it is "{type}_{id}", which avoids putting
// credentials in filenames while still grouping per stream.
func channelKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "misc"
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) == 0 || segs[0] == "" {
		return "misc"
	}
	if len(segs) >= 4 && isXtreamType(segs[0]) {
		id := strings.TrimSuffix(segs[len(segs)-1], segExt(rawURL))
		return sanitize(segs[0] + "_" + id)
	}
	return sanitize(segs[0])
}

func isXtreamType(s string) bool {
	switch s {
	case "live", "movie", "series", "timeshift":
		return true
	}
	return false
}

func segExt(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return path.Ext(u.Path)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}

// ensure io is referenced (Entry implements io.Writer).
var _ io.Writer = (*Entry)(nil)
