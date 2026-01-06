package storage

import (
	"encoding/json"
	"os"
	"sync"
)

// VisitCounter manages a thread-safe visit counter with file persistence.
type VisitCounter struct {
	mu       sync.RWMutex
	count    int64
	filePath string
}

type visitData struct {
	Count int64 `json:"count"`
}

// NewVisitCounter creates a new counter, loading existing count from file if present.
func NewVisitCounter(filePath string) (*VisitCounter, error) {
	vc := &VisitCounter{
		filePath: filePath,
	}

	// Try to load existing count
	if err := vc.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return vc, nil
}

// load reads the count from the JSON file.
func (vc *VisitCounter) load() error {
	data, err := os.ReadFile(vc.filePath)
	if err != nil {
		return err
	}

	var vd visitData
	if err := json.Unmarshal(data, &vd); err != nil {
		return err
	}

	vc.count = vd.Count
	return nil
}

// save writes the current count to the JSON file.
func (vc *VisitCounter) save() error {
	data, err := json.Marshal(visitData{Count: vc.count})
	if err != nil {
		return err
	}

	return os.WriteFile(vc.filePath, data, 0644)
}

// Increment adds one to the counter and persists to file.
func (vc *VisitCounter) Increment() int64 {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.count++

	// Fire-and-forget save - don't block on file I/O errors
	go func(count int64) {
		vc.mu.RLock()
		defer vc.mu.RUnlock()
		_ = vc.save()
	}(vc.count)

	return vc.count
}

// Get returns the current count without incrementing.
func (vc *VisitCounter) Get() int64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.count
}
