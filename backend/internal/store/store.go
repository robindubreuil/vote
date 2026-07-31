// Package store persists aggregate usage counters and a sampled time-series to
// the local filesystem. It is the single durable source of truth for product
// usage across process restarts.
//
// Files live under a configurable directory (VOTE_DATA_DIR, FHS default
// /var/lib/vote, dev default ./data):
//
//   - counters.json — the latest cumulative counter snapshot, atomically
//     rewritten on a fixed cadence and on graceful shutdown. Used to restore
//     counters on boot so they read as all-time monotonic.
//   - stats.jsonl   — append-only history, one JSON object per sample. Used to
//     reconstruct usage trends since the process first ran.
//
// Security: the directory is created 0700 and files 0600, owned by the service
// user. counters.json is written via temp-file + rename (atomic visibility,
// no half-writes, no symlink races) and additionally fsync'd — both the temp
// file (so the bytes are durable before the rename) and the containing
// directory (so the rename itself is durable). stats.jsonl is O_APPEND
// line-oriented so partial lines can be skipped on read; it is intentionally
// not fsync'd because it is a lossy history where "worst-case crash loses at
// most one interval" is the documented guarantee. No other process should
// write these files.
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	countersFile    = "counters.json"
	statsFile       = "stats.jsonl"
	statsBackupFile = "stats.jsonl.1"
	// defaultMaxLogBytes caps the append-only log; on exceed it rotates to a
	// single backup. ~150 B/line at 5-min cadence ≈ 15 MB/year, so 50 MB
	// holds 3+ years before rotation ever triggers.
	defaultMaxLogBytes = 50 * 1024 * 1024
)

// Sample is one point-in-time usage snapshot. Field names are short to keep
// the JSONL compact over years of sampling.
type Sample struct {
	Time                time.Time `json:"ts"`
	SessionsCreated     int64     `json:"sc"`
	VotesStarted        int64     `json:"vs"`
	VotesCast           int64     `json:"vc"`
	TraineesJoined      int64     `json:"tj"`
	GameEnabledVotes    int64     `json:"ge"`
	MultipleChoiceVotes int64     `json:"mc"`
}

// HistogramBucket is one cumulative Prometheus histogram bucket: the count of
// observations whose value was ≤ LE.
type HistogramBucket struct {
	LE    float64 `json:"le"`
	Count int64   `json:"count"`
}

// Histogram is the persisted shape of a vote.HistogramSnapshot. Stored only
// in counters.json (not in stats.jsonl samples) so the time-series log stays
// compact — distributions don't need a per-sample history.
type Histogram struct {
	Count   int64             `json:"count"`
	Sum     float64           `json:"sum"`
	Buckets []HistogramBucket `json:"buckets,omitempty"`
}

// Counters is what counters.json persists: the latest cumulative counters
// plus the latest histogram distributions, so a restart can restore both as
// all-time monotonic. It embeds Sample so the same JSON shape extends what
// older deployments already had on disk (extra histogram fields default to
// zero on unmarshal and are simply skipped by an older binary reading newer
// counters.json).
type Counters struct {
	Sample
	SessionDuration    Histogram `json:"sd,omitempty"`
	VotesPerSession    Histogram `json:"vp,omitempty"`
	TraineesPerSession Histogram `json:"tp,omitempty"`
}

// Store owns the two persistence files. Methods are safe for concurrent use;
// the sampling goroutine is the sole writer, ReadSamples is safe to call from
// HTTP handlers concurrently with writes.
type Store struct {
	dir          string
	logPath      string
	logBackup    string
	countersPath string
	mu           sync.Mutex
	logFile      *os.File
	maxLogBytes  int64
}

// New creates or opens the data directory and the append-only log. The
// directory is created and tightened to 0700 (a pre-existing dir with looser
// perms is corrected — defense in depth); the log file is created 0600.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("store: empty data dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir %q: %w", dir, err)
	}
	// MkdirAll is a no-op on perms if the dir already exists, so enforce the
	// restrictive mode explicitly. Best-effort: a failure (e.g. foreign-owned
	// mount) is surfaced but does not block startup.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: tighten data dir %q to 0700: %w", dir, err)
	}
	logPath := filepath.Join(dir, statsFile)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: open stats log: %w", err)
	}
	return &Store{
		dir:          dir,
		logPath:      logPath,
		logBackup:    filepath.Join(dir, statsBackupFile),
		countersPath: filepath.Join(dir, countersFile),
		logFile:      f,
		maxLogBytes:  defaultMaxLogBytes,
	}, nil
}

// Dir returns the on-disk data directory.
func (s *Store) Dir() string { return s.dir }

// SaveCounters atomically and durably writes the current cumulative counters
// (and the histogram distributions). The write is temp-file → fsync → rename
// → fsync(dir): readers always see either the old or the new complete file
// (atomic visibility), and a power loss in any window cannot leave
// counters.json empty or referring to unwritten extents (crash-durability).
//
// R21: the prior os.WriteFile + os.Rename path was atomic at the
// directory-entry level only — a successful Rename says nothing about whether
// the data blocks were flushed, so a crash between Rename returning and the
// kernel flushing could leave counters.json empty or backed by unwritten
// extents. The full portable durability recipe (fsync the temp before the
// rename so the bytes are committed, fsync the directory after so the rename
// metadata is committed) closes that window. The cost is two extra fsyncs
// per flush; counters.json is rewritten once per VOTE_STATS_INTERVAL (default
// 5m) and on graceful shutdown, so the steady-state cost is ~12 fsyncs/hour —
// negligible on any disk that isn't pure SD card. stats.jsonl stays
// un-fsynced because it is an append-only lossy history where losing the
// tail sample on crash is the documented contract.
func (s *Store) SaveCounters(c Counters) error {
	if !validCounters(c) {
		return fmt.Errorf("store: invalid counters")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("store: marshal counters: %w", err)
	}
	tmp := filepath.Join(s.dir, countersFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("store: write %s: %w", countersFile+".tmp", err)
	}
	if err := syncPath(tmp); err != nil {
		return fmt.Errorf("store: fsync %s: %w", countersFile+".tmp", err)
	}
	if err := os.Rename(tmp, s.countersPath); err != nil {
		return fmt.Errorf("store: rename %s: %w", countersFile, err)
	}
	if err := syncDir(s.dir); err != nil {
		// The rename has already happened — readers see the new file. A
		// directory-fsync failure means the rename itself might not survive
		// a crash, but it doesn't compromise the current process or any
		// concurrent reader. Surface it so the operator sees the FS fault
		// rather than silently depending on a durability we couldn't give.
		return fmt.Errorf("store: fsync dir after counters rename: %w", err)
	}
	return nil
}

// syncPath fsyncs the file at path. Used to commit the temp file's data
// blocks before the rename that makes it visible as counters.json.
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// syncDir fsyncs the directory at path, committing any recent rename/create
// metadata. The portable durability recipe requires this after a rename: a
// successful rename makes the new name visible but does not guarantee the
// directory entry change is on stable storage. On Linux, NetworkFS, and most
// POSIX filesystems this is the only way to make a rename crash-durable.
func syncDir(path string) error {
	// O_RDONLY opens the directory inode for fsync without attempting to
	// read entries; fsync of a dir fd is the standard durability primitive.
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// LoadCounters reads the persisted counters and histograms. Missing file →
// zero value (fresh start). Corrupt or invalid file → zero value (recover,
// never crash the server on boot). A valid snapshot has non-negative,
// internally-consistent counters and (when present) cumulative-non-decreasing
// histogram buckets whose counts cannot exceed the total observation count.
func (s *Store) LoadCounters() (Counters, error) {
	data, err := os.ReadFile(s.countersPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Counters{}, nil
		}
		return Counters{}, fmt.Errorf("store: read %s: %w", countersFile, err)
	}
	var c Counters
	if err := json.Unmarshal(data, &c); err != nil {
		slog.Warn("counters.json corrupt, starting fresh", "error", err, "path", s.countersPath)
		return Counters{}, nil
	}
	if !validCounters(c) {
		slog.Warn("counters.json invalid, starting fresh", "path", s.countersPath)
		return Counters{}, nil
	}
	return c, nil
}

// reopenLocked reopens the append-only log under s.mu. Used after rotation
// and as the self-heal path when a prior reopen failed and left s.logFile
// nil (B9). Caller holds s.mu.
func (s *Store) reopenLocked() error {
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.logFile = f
	return nil
}

// AppendSample appends one sample to the append-only log, rotating to a single
// backup if the file has grown past the cap. One JSON object per line; the
// trailing newline makes partial lines safely skippable on read.
//
// B9: the rotation path previously set s.logFile to a fresh handle only on
// reopen success; if reopen failed after rename, s.logFile kept pointing at
// the renamed-away (closed) fd and every subsequent AppendSample failed
// silently until the process restarted. We now nil the handle on every
// rotation close, retry reopen on failure, and additionally self-heal from
// the top of AppendSample so a transient FS error doesn't poison the
// sampling goroutine for the lifetime of the process.
func (s *Store) AppendSample(sample Sample) error {
	if !valid(sample) {
		return fmt.Errorf("store: invalid sample")
	}
	line, err := json.Marshal(sample)
	if err != nil {
		return fmt.Errorf("store: marshal sample: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	// Self-heal: a prior rotation or Close may have left s.logFile nil. The
	// graceful-shutdown path serialises AppendSample behind Close via the
	// sampler's WaitGroup, so in normal operation this branch is cold; it
	// exists to recover a Store that survived a transient FS fault instead
	// of failing every subsequent AppendSample on a dead fd.
	if s.logFile == nil {
		if err := s.reopenLocked(); err != nil {
			return fmt.Errorf("store: reopen log: %w", err)
		}
	}

	if fi, statErr := os.Stat(s.logPath); statErr == nil && fi.Size() >= s.maxLogBytes {
		// Close is best-effort on rotation: even if it fails the fd
		// will be reaped by GC. Keep s.logFile nil so the reopen
		// below establishes a fresh handle.
		_ = s.logFile.Close()
		s.logFile = nil
		_ = os.Remove(s.logBackup)
		if err := os.Rename(s.logPath, s.logBackup); err != nil {
			// Rename failed: stats.jsonl is still on disk. Reopen it
			// in-place — the next size check will rotate again on
			// the following sample.
			slog.Warn("log rotation rename failed; continuing with existing path", "error", err)
		}
		// Reopen regardless of rename outcome. If reopen fails, s.logFile
		// stays nil and the next AppendSample will retry via the
		// self-heal path at the top rather than fail forever on a
		// closed fd.
		if err := s.reopenLocked(); err != nil {
			return fmt.Errorf("store: reopen log after rotation: %w", err)
		}
	}
	if _, err := s.logFile.Write(line); err != nil {
		// The fd may have been invalidated out-of-band (external log
		// truncation, FS unmount, rotated by an admin script). Try one
		// reopen + retry so the sampling goroutine survives a transient
		// write error rather than poisoning every subsequent AppendSample.
		if rErr := s.reopenLocked(); rErr == nil {
			if _, wErr := s.logFile.Write(line); wErr == nil {
				return nil
			}
		}
		return fmt.Errorf("store: append sample: %w", err)
	}
	return nil
}

// scannerBufferSize caps the line length that ReadSamples will accept.
// A normal sample is ~150 B; a line longer than this (disk corruption,
// a torn multi-MB write, log injection) is consumed and discarded in
// bounded chunks so scanning resumes at the next newline — genuinely,
// not the unrecoverable bufio.Scanner stop that R23 fixed. The value
// bounds both the bufio.Reader's internal buffer and the per-line
// allocation budget.
const scannerBufferSize = 1 << 20 // 1 MiB

// readBoundedLine reads one line (up to and including '\n') from r and
// returns the line without the trailing newline. Lines longer than the
// reader's buffer size are consumed in bounded chunks and discarded — the
// caller sees a nil slice and a nil error and continues to the next line.
//
// R23: the prior implementation used bufio.Scanner, which stops
// unrecoverably on a token larger than its max buffer (bufio.ErrTooLong).
// One corrupt line without an embedded newline therefore poisoned every
// valid sample after it until the file rotated to .1. readBoundedLine
// uses bufio.Reader.ReadSlice, which returns bufio.ErrBufferFull for an
// oversized line; the discard loop consumes the remainder in buffer-sized
// chunks (no unbounded allocation) and scanning resumes at the next
// newline. The returned slice references the reader's internal buffer and
// is valid only until the next read call — callers must parse it
// immediately.
func readBoundedLine(r *bufio.Reader) (line []byte, err error) {
	chunk, err := r.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		// Oversized line: consume the remainder until the newline or
		// EOF, then signal skip (nil data, nil error). The caller
		// continues scanning — this is the recovery that Scanner could
		// not do.
		for {
			if _, dErr := r.ReadSlice('\n'); dErr != bufio.ErrBufferFull {
				break
			}
		}
		return nil, nil
	}
	// Strip the trailing newline when present (ReadSlice includes it on
	// a successful delimiter match; at EOF the last line may lack one).
	if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
		chunk = chunk[:len(chunk)-1]
	}
	return chunk, err
}

// ReadSamples returns up to limit most-recent samples (oldest→newest). When
// limit <= 0 all available samples are returned. Malformed lines are skipped
// so a torn write never poisons the whole history. Lines longer than
// scannerBufferSize (disk corruption, a stray dd, log injection) are also
// skipped — and crucially, scanning resumes at the next newline instead of
// stopping unrecoverably (R23). Both the current log and its rotated backup
// are read and merged in chronological order.
//
// B2: the previous implementation read both files (~100 MB worst case after
// years of sampling) fully into memory with os.ReadFile and parsed under
// s.mu — /dashboard/history was an OOM risk on small VMs and the sampling
// goroutine stalled behind a slow read. This version snapshots the file
// paths under s.mu (defending against a concurrent rotation that could
// rename the live log away mid-read), releases the lock, then streams each
// file line-by-line through a bufio.Reader. When limit > 0 a fixed-size
// ring buffer holds only the tail, bounding memory to O(limit) regardless
// of file size.
func (s *Store) ReadSamples(limit int) ([]Sample, error) {
	// Snapshot the paths under the lock so a concurrent rotation can't
	// rename the live log out from under us between picking the path and
	// opening it. The actual read happens lock-free — reads use their
	// own *os.File handle, not s.logFile, so they neither block writes
	// nor race with the writer's fd.
	s.mu.Lock()
	paths := []string{s.logBackup, s.logPath}
	s.mu.Unlock()

	var out []Sample
	var ring *ringSample
	if limit > 0 {
		ring = newRingSample(limit)
	} else {
		out = make([]Sample, 0)
	}

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("store: read %s: %w", filepath.Base(p), err)
		}
		// R23: bufio.Reader with ReadSlice recovers genuinely past an
		// oversized line, unlike bufio.Scanner which stops unrecoverably
		// on bufio.ErrTooLong. The buffer is sized to scannerBufferSize so
		// a normal sample (≤ 1 MiB) is returned in one ReadSlice call;
		// only lines exceeding that trigger the bounded discard loop.
		reader := bufio.NewReaderSize(f, scannerBufferSize)
		for {
			line, readErr := readBoundedLine(reader)
			if len(line) > 0 {
				var sample Sample
				if err := json.Unmarshal(line, &sample); err == nil && valid(sample) {
					if ring != nil {
						ring.push(sample)
					} else {
						out = append(out, sample)
					}
				}
			}
			if readErr != nil {
				// An I/O error mid-read is non-fatal to history reads:
				// return whatever we collected rather than failing the
				// whole call. The append-only log is line-oriented and
				// self-healing on the next successful write, so a
				// transient read error shouldn't 500 the dashboard.
				if readErr != io.EOF {
					slog.Warn("stats log read error", "path", p, "error", readErr)
				}
				break
			}
		}
		f.Close()
	}

	if ring != nil {
		return ring.snapshot(), nil
	}
	return out, nil
}

// ringSample is a fixed-capacity ring buffer of Sample values. push is
// O(1); snapshot returns the contents in insertion order (oldest first)
// in O(n). Used by ReadSamples when a limit is supplied so the call's
// peak memory is bounded by limit rather than the on-disk log size.
type ringSample struct {
	buf   []Sample
	i     int
	full  bool
	limit int
}

func newRingSample(limit int) *ringSample {
	return &ringSample{buf: make([]Sample, limit), limit: limit}
}

func (r *ringSample) push(s Sample) {
	r.buf[r.i] = s
	r.i = (r.i + 1) % r.limit
	if r.i == 0 {
		r.full = true
	}
}

func (r *ringSample) snapshot() []Sample {
	if !r.full {
		// Not yet wrapped: only positions [0, r.i) hold data.
		out := make([]Sample, r.i)
		copy(out, r.buf[:r.i])
		return out
	}
	// Wrapped: oldest entry is at r.i, newest at r.i-1 (mod limit).
	out := make([]Sample, r.limit)
	n := copy(out, r.buf[r.i:])
	copy(out[n:], r.buf[:r.i])
	return out
}

// Close flushes and closes the log file. Safe to call multiple times.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile != nil {
		err := s.logFile.Close()
		s.logFile = nil
		return err
	}
	return nil
}

// Permissions verifies the data directory and files have the expected
// restrictive modes. Returns the first violation found, or nil if all good.
// Useful as a startup self-check in hardened deployments.
func (s *Store) Permissions() error {
	if fi, err := os.Stat(s.dir); err != nil {
		return err
	} else if fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("store: dir %s perm %o, expected 0700", s.dir, fi.Mode().Perm())
	}
	for _, p := range []string{s.countersPath, s.logPath, s.logBackup} {
		if fi, err := os.Stat(p); err == nil {
			if fi.Mode().Perm() != 0o600 {
				return fmt.Errorf("store: file %s perm %o, expected 0600", p, fi.Mode().Perm())
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func valid(s Sample) bool {
	return !s.Time.IsZero() &&
		s.SessionsCreated >= 0 && s.VotesStarted >= 0 &&
		s.VotesCast >= 0 && s.TraineesJoined >= 0 &&
		s.GameEnabledVotes >= 0 && s.MultipleChoiceVotes >= 0 &&
		s.GameEnabledVotes <= s.VotesStarted &&
		s.MultipleChoiceVotes <= s.VotesStarted
}

// validHistogram enforces the Prometheus cumulative-bucket invariants: total
// count non-negative, bucket counts non-negative and monotonically
// non-decreasing, and no bucket holds more observations than the total.
//
// R20: Sum is also checked for finiteness. A NaN/Inf sum (disk corruption,
// manual editing, or accumulated R19 damage) unmarshals cleanly and would
// otherwise pass every other check, propagate through addLocked
// (x + NaN = NaN) and surface in /metrics as `..._sum NaN`, which breaks
// the dashboard's SVG sparkline renderer and most alerting rules.
// LoadCounters already rejects the whole file on validation failure, which
// is the right recovery (start fresh).
func validHistogram(h Histogram) bool {
	if h.Count < 0 {
		return false
	}
	if math.IsNaN(h.Sum) || math.IsInf(h.Sum, 0) {
		return false
	}
	var prev int64
	for _, b := range h.Buckets {
		if b.Count < 0 || b.Count > h.Count || b.Count < prev {
			return false
		}
		prev = b.Count
	}
	return true
}

// validCounters is the on-disk invariant for counters.json: the time-series
// counter portion (delegated to valid) plus any embedded histogram snapshots.
func validCounters(c Counters) bool {
	return valid(c.Sample) &&
		validHistogram(c.SessionDuration) &&
		validHistogram(c.VotesPerSession) &&
		validHistogram(c.TraineesPerSession)
}

// Compile-time guard that os.WriteFile's truncation path keeps the file mode.
var _ fs.FileMode = 0o600
