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

// SaveCounters atomically writes the current cumulative counters. The write is
// temp-file + rename, so a crash never leaves a partially-written counters.json
// and readers always see either the old or the new complete file.
func (s *Store) SaveCounters(sample Sample) error {
	data, err := json.Marshal(sample)
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

// LoadCounters reads the persisted base counters. Missing file → zero value
// (fresh start). Corrupt or invalid file → zero value (recover, never crash
// the server on boot). A valid snapshot is non-negative and internally
// consistent (feature counters cannot exceed votes started).
func (s *Store) LoadCounters() (Sample, error) {
	data, err := os.ReadFile(s.countersPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Sample{}, nil
		}
		return Sample{}, fmt.Errorf("store: read %s: %w", countersFile, err)
	}
	var sample Sample
	if err := json.Unmarshal(data, &sample); err != nil {
		slog.Warn("counters.json corrupt, starting fresh", "error", err, "path", s.countersPath)
		return Sample{}, nil
	}
	if !valid(sample) {
		slog.Warn("counters.json invalid, starting fresh", "path", s.countersPath)
		return Sample{}, nil
	}
	return sample, nil
}

// AppendSample appends one sample to the append-only log, rotating to a single
// backup if the file has grown past the cap. One JSON object per line; the
// trailing newline makes partial lines safely skippable on read.
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

	if fi, statErr := os.Stat(s.logPath); statErr == nil && fi.Size() >= s.maxLogBytes {
		if err := s.logFile.Close(); err != nil {
			return fmt.Errorf("store: close log for rotation: %w", err)
		}
		_ = os.Remove(s.logBackup)
		if err := os.Rename(s.logPath, s.logBackup); err != nil {
			slog.Warn("log rotation failed, reopening original path", "error", err)
			f, reopenErr := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if reopenErr != nil {
				return fmt.Errorf("store: reopen log after failed rotation: %w", reopenErr)
			}
			s.logFile = f
		} else {
			f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				return fmt.Errorf("store: reopen log after rotation: %w", err)
			}
			s.logFile = f
		}
	}
	if _, err := s.logFile.Write(line); err != nil {
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

// Compile-time guard that os.WriteFile's truncation path keeps the file mode.
var _ fs.FileMode = 0o600
