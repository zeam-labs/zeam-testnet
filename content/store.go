package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type ContentID [32]byte

func HashData(data []byte) ContentID {
	return sha256.Sum256(data)
}

func (c ContentID) Hex() string {
	return hex.EncodeToString(c[:])
}

func (c ContentID) String() string {
	return c.Hex()
}

func ParseContentID(s string) (ContentID, error) {
	var id ContentID
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, fmt.Errorf("invalid content ID: %w", err)
	}
	if len(b) != 32 {
		return id, fmt.Errorf("content ID must be 32 bytes, got %d", len(b))
	}
	copy(id[:], b)
	return id, nil
}

type Store struct {
	mu       sync.RWMutex
	chunks   map[ContentID][]byte
	diskPath string
}

func NewStore(diskPath string) *Store {
	return &Store{
		chunks:   make(map[ContentID][]byte),
		diskPath: diskPath,
	}
}

func (s *Store) Has(id ContentID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.chunks[id]
	return ok
}

func (s *Store) Get(id ContentID) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.chunks[id]
	return data, ok
}

func (s *Store) Put(data []byte) ContentID {
	id := HashData(data)
	s.mu.Lock()
	s.chunks[id] = data
	s.mu.Unlock()
	return id
}

func (s *Store) PutWithID(id ContentID, data []byte) {
	s.mu.Lock()
	s.chunks[id] = data
	s.mu.Unlock()
}

func (s *Store) Delete(id ContentID) {
	s.mu.Lock()
	delete(s.chunks, id)
	s.mu.Unlock()
}

func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks)
}

func (s *Store) Keys() []ContentID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]ContentID, 0, len(s.chunks))
	for k := range s.chunks {
		keys = append(keys, k)
	}
	return keys
}

func (s *Store) TotalBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	for _, data := range s.chunks {
		total += int64(len(data))
	}
	return total
}

func (s *Store) Flush() error {
	if s.diskPath == "" {
		return nil
	}
	if err := os.MkdirAll(s.diskPath, 0755); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, data := range s.chunks {
		path := filepath.Join(s.diskPath, id.Hex())
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("flush %s: %w", id.Hex(), err)
		}
	}
	return nil
}

func (s *Store) Load() error {
	if s.diskPath == "" {
		return nil
	}
	entries, err := os.ReadDir(s.diskPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id, err := ParseContentID(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.diskPath, entry.Name()))
		if err != nil {
			continue
		}
		s.chunks[id] = data
	}
	return nil
}
