package task

// RegisterDefinition adds or replaces one task definition on an initialized
// Queue. Registration is serialized with enqueue/claim state so server-owned
// optional task domains can install their definition before workers snapshot
// TypesWithHandlers, without exposing Registry internals.
func (q *Queue) RegisterDefinition(taskType string, definition Definition) error {
	if q == nil || q.registry == nil {
		return ErrClosed
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.registry.register(taskType, definition)
}
