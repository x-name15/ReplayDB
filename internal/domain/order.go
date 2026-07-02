package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// OrderState represents the current state of our aggregate
type OrderState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     float64   `json:"total"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   uint32    `json:"version"` // 
}

// NewOrderState initializes a clean entity with Version 0
func NewOrderState(id string) *OrderState {
	return &OrderState{
		ID:      id,
		Status:  "PENDING",
		Version: 0,
	}
}

// Apply mutates the state based on historical events
func (s *OrderState) Apply(eventType string, payload []byte, timestamp time.Time) error {
	switch eventType {
	case "OrderCreated":
		var data struct {
			Total    float64 `json:"total"`
			Currency string  `json:"currency"`
		}
		if err := json.Unmarshal(payload, &data); err != nil {
			return err
		}
		s.Total = data.Total
		s.Currency = data.Currency
		s.Status = "CREATED"

	case "OrderPaid":
		s.Status = "PAID"

	case "OrderShipped":
		s.Status = "SHIPPED"

	default:
		return fmt.Errorf("unrecognized event type: %s", eventType)
	}

	// 👈 FASE 5: Cada vez que aplicamos un evento válido, la versión del estado incrementa en 1.
	// Esto es lo que permite al Time-Travel Engine saltarse eventos antiguos.
	s.Version++
	s.UpdatedAt = timestamp

	return nil
}