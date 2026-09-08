package generation

import "sync"

type MemoryRunStore struct {
	mu       sync.Mutex
	runs     map[string]*Run
	results  map[string][]Result
	sequence int
}

func NewMemoryRunStore() *MemoryRunStore {
	return &MemoryRunStore{runs: map[string]*Run{}, results: map[string][]Result{}}
}
