package domain

import (
	"fmt"
	"time"
)

type Factory func(id string) Aggregate

type Aggregate interface {
	Apply(eventType string, payload []byte, timestamp time.Time) error
	Version() uint32
}

type Registry struct {
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

func (r *Registry) Register(kind string, factory Factory) {
	if _, exists := r.factories[kind]; exists {
		panic(fmt.Sprintf("domain: aggregate kind %q already registered", kind))
	}
	r.factories[kind] = factory
}

func (r *Registry) New(kind, id string) (Aggregate, error) {
	factory, ok := r.factories[kind]
	if !ok {
		return nil, fmt.Errorf("domain: unknown aggregate kind %q (did you forget to Register it?)", kind)
	}
	return factory(id), nil
}
