package scan

// NewSlots exposes the per-host semaphore to the black-box tests.
var NewSlots = newSlots

// Limit returns the current number of slots.
func (s *slots) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}
