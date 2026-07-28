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
// user. counters.json is written via temp-file + rename (atomic, no half-writes,
// no symlink races). stats.jsonl is O_APPEND line-oriented so partial lines can
// be skipped on read. No other process should write these files.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
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

// SaveCounters atomically writes the current cumulative counters (and the
// histogram distributions). The write is temp-file + rename, so a crash never
// leaves a partially-written counters.json and readers always see either the
// old or the new complete file.
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
	if err := os.Rename(tmp, s.countersPath); err != nil {
		return fmt.Errorf("store: rename %s: %w", countersFile, err)
	}
	return nil
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
		if err := s.logFile.Close(); err != nil {
			// Close is best-effort on rotation: even if it fails the fd
			// will be reaped by GC. Keep s.logFile nil so the reopen
			// below establishes a fresh handle.
			s.logFile = nil
		} else {
			s.logFile = nil
		}
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

// ReadSamples returns up to limit most-recent samples (oldest→newest). When
// limit <= 0 all available samples are returned. Malformed lines are skipped so
// a torn write never poisons the whole history. Both the current log and its
// rotated backup are read and merged in chronological order.
func (s *Store) ReadSamples(limit int) ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Sample
	for _, p := range []string{s.logBackup, s.logPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("store: read %s: %w", filepath.Base(p), err)
		}
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var sample Sample
			if err := json.Unmarshal(line, &sample); err != nil {
				continue
			}
			if valid(sample) {
				out = append(out, sample)
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
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
func validHistogram(h Histogram) bool {
	if h.Count < 0 {
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
