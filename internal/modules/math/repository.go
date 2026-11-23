package math

import "sync"

// Repository defines persistence operations for calculation history.
type Repository interface {
	Save(entry CalculationEntry)
	Count() int
	List() []CalculationEntry
}

// InMemoryRepository stores calculation entries in memory.
type InMemoryRepository struct {
	mu      sync.Mutex
	entries []CalculationEntry
}

// NewInMemoryRepository constructs an empty calculation store.
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{}
}

// Save appends a calculation entry.
func (r *InMemoryRepository) Save(entry CalculationEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

// Count returns the total number of stored entries.
func (r *InMemoryRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// List returns a copy of stored calculation entries.
func (r *InMemoryRepository) List() []CalculationEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CalculationEntry, len(r.entries))
	copy(out, r.entries)
	return out
}
