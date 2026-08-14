package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/yinhm/friendfeed/pb"
)

const DefaultMaxPayloadBytes = 64 << 10

type Handler func(context.Context, *pb.Task) error

type Definition struct {
	ValidatePayload func([]byte, uint32) error
	MaxPayloadBytes int
	MaxAttempts     uint32
	LeaseDuration   time.Duration
	MaxLease        time.Duration
	BackoffBase     time.Duration
	BackoffCap      time.Duration
	Handler         Handler
}

type Registry struct {
	definitions map[string]Definition
}

func NewRegistry(definitions map[string]Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]Definition, len(definitions))}
	for taskType, definition := range definitions {
		if err := registry.register(taskType, definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Definition(taskType string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	definition, ok := r.definitions[taskType]
	return definition, ok
}

func (r *Registry) TypesWithHandlers() []string {
	if r == nil {
		return nil
	}
	types := make([]string, 0, len(r.definitions))
	for taskType, definition := range r.definitions {
		if definition.Handler != nil {
			types = append(types, taskType)
		}
	}
	slices.Sort(types)
	return types
}

func (r *Registry) Validate(taskType string, payload []byte, version uint32) (Definition, error) {
	definition, ok := r.Definition(taskType)
	if !ok {
		return Definition{}, fmt.Errorf("unknown task type %q", taskType)
	}
	if len(payload) > definition.MaxPayloadBytes {
		return Definition{}, fmt.Errorf("task payload is %d bytes, limit %d", len(payload), definition.MaxPayloadBytes)
	}
	if definition.ValidatePayload != nil {
		if err := definition.ValidatePayload(payload, version); err != nil {
			return Definition{}, fmt.Errorf("validate %s payload: %w", taskType, err)
		}
	}
	return definition, nil
}

func (r *Registry) register(taskType string, definition Definition) error {
	if err := ValidateType(taskType); err != nil {
		return err
	}
	if definition.MaxAttempts == 0 {
		return errors.New("task MaxAttempts must be positive")
	}
	if definition.LeaseDuration <= 0 || definition.MaxLease <= 0 || definition.LeaseDuration > definition.MaxLease {
		return fmt.Errorf("task %s requires 0 < LeaseDuration <= MaxLease", taskType)
	}
	if definition.BackoffBase <= 0 || definition.BackoffCap <= 0 || definition.BackoffBase > definition.BackoffCap {
		return fmt.Errorf("task %s requires 0 < BackoffBase <= BackoffCap", taskType)
	}
	if definition.MaxPayloadBytes == 0 {
		definition.MaxPayloadBytes = DefaultMaxPayloadBytes
	}
	if definition.MaxPayloadBytes < 0 {
		return fmt.Errorf("task %s MaxPayloadBytes must not be negative", taskType)
	}
	r.definitions[taskType] = definition
	return nil
}
