package memory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const numLockShards = 64

// maxLineSize is the maximum size of a single JSON line in a .jsonl file.
const maxLineSize = 10 * 1024 * 1024 // 10 MB

type sessionMeta struct {
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Skip      int       `json:"skip"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JSONLStore implements Store using append-only JSONL files.
type JSONLStore struct {
	dir   string
	locks [numLockShards]sync.Mutex
}

func NewJSONLStore(dir string) (*JSONLStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory: create directory: %w", err)
	}
	return &JSONLStore{dir: dir}, nil
}

func (s *JSONLStore) sessionLock(key string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(key))
	return &s.locks[h.Sum32()%numLockShards]
}

func (s *JSONLStore) jsonlPath(key string) string {
	return filepath.Join(s.dir, sanitizeKey(key)+".jsonl")
}

func (s *JSONLStore) metaPath(key string) string {
	return filepath.Join(s.dir, sanitizeKey(key)+".meta.json")
}

func sanitizeKey(key string) string {
	return strings.ReplaceAll(key, ":", "_")
}

func (s *JSONLStore) readMeta(key string) (sessionMeta, error) {
	data, err := os.ReadFile(s.metaPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return sessionMeta{Key: key}, nil
	}
	if err != nil {
		return sessionMeta{}, fmt.Errorf("memory: read meta: %w", err)
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return sessionMeta{}, fmt.Errorf("memory: decode meta: %w", err)
	}
	return meta, nil
}

func (s *JSONLStore) writeMeta(key string, meta sessionMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encode meta: %w", err)
	}
	return fileutil.WriteFileAtomic(s.metaPath(key), data, 0o644)
}

// withJSONLFile opens a jsonl file for reading, handling ErrNotExist.
func withJSONLFile(path string, fn func(*bufio.Scanner) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("memory: open jsonl: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	return fn(scanner)
}

// readMessages reads valid JSON lines from a .jsonl file.
func readMessages(path string, skip int) ([]providers.Message, error) {
	var msgs []providers.Message
	lineNum := 0

	err := withJSONLFile(path, func(scanner *bufio.Scanner) error {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			lineNum++
			if lineNum <= skip {
				continue
			}
			var msg providers.Message
			if err := json.Unmarshal(line, &msg); err != nil {
				log.Printf("memory: skipping corrupt line %d in %s: %v",
					lineNum, filepath.Base(path), err)
				continue
			}
			msgs = append(msgs, msg)
		}
		return scanner.Err()
	})

	if err != nil {
		return nil, err
	}
	if msgs == nil {
		msgs = []providers.Message{}
	}
	return msgs, nil
}

// countLines counts non-empty lines in a .jsonl file.
func countLines(path string) (int, error) {
	n := 0
	err := withJSONLFile(path, func(scanner *bufio.Scanner) error {
		for scanner.Scan() {
			if len(scanner.Bytes()) > 0 {
				n++
			}
		}
		return nil
	})
	return n, err
}

func (s *JSONLStore) AddMessage(_ context.Context, sessionKey, role, content string) error {
	return s.addMsg(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

func (s *JSONLStore) AddFullMessage(_ context.Context, sessionKey string, msg providers.Message) error {
	return s.addMsg(sessionKey, msg)
}

func (s *JSONLStore) addMsg(sessionKey string, msg providers.Message) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	f, err := os.OpenFile(s.jsonlPath(sessionKey), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("memory: open jsonl: %w", err)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(msg); err != nil {
		f.Close()
		return fmt.Errorf("memory: encode message: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("memory: sync jsonl: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("memory: close jsonl: %w", err)
	}

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.Count == 0 && meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Count++
	meta.UpdatedAt = now
	return s.writeMeta(sessionKey, meta)
}

func (s *JSONLStore) GetHistory(_ context.Context, sessionKey string) ([]providers.Message, error) {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return nil, err
	}
	return readMessages(s.jsonlPath(sessionKey), meta.Skip)
}

func (s *JSONLStore) GetSummary(_ context.Context, sessionKey string) (string, error) {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return "", err
	}
	return meta.Summary, nil
}

func (s *JSONLStore) SetSummary(_ context.Context, sessionKey, summary string) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Summary = summary
	meta.UpdatedAt = now
	return s.writeMeta(sessionKey, meta)
}

func (s *JSONLStore) TruncateHistory(_ context.Context, sessionKey string, keepLast int) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}

	n, err := countLines(s.jsonlPath(sessionKey))
	if err != nil {
		return err
	}
	meta.Count = n

	if keepLast <= 0 {
		meta.Skip = meta.Count
	} else if keepLast < meta.Count-meta.Skip {
		meta.Skip = meta.Count - keepLast
	}
	meta.UpdatedAt = time.Now()
	return s.writeMeta(sessionKey, meta)
}

func (s *JSONLStore) SetHistory(
	_ context.Context, sessionKey string, history []providers.Message,
) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Skip = 0
	meta.Count = len(history)
	meta.UpdatedAt = now

	if err := s.writeMeta(sessionKey, meta); err != nil {
		return err
	}
	return s.rewriteJSONL(sessionKey, history)
}

func (s *JSONLStore) Compact(_ context.Context, sessionKey string) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	if meta.Skip == 0 {
		return nil
	}

	active, err := readMessages(s.jsonlPath(sessionKey), meta.Skip)
	if err != nil {
		return err
	}

	meta.Skip = 0
	meta.Count = len(active)
	meta.UpdatedAt = time.Now()

	if err := s.writeMeta(sessionKey, meta); err != nil {
		return err
	}
	return s.rewriteJSONL(sessionKey, active)
}

func (s *JSONLStore) rewriteJSONL(sessionKey string, msgs []providers.Message) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("memory: encode message: %w", err)
		}
	}
	return fileutil.WriteFileAtomic(s.jsonlPath(sessionKey), buf.Bytes(), 0o644)
}

func (s *JSONLStore) Close() error {
	return nil
}
