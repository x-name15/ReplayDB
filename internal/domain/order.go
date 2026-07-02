package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

type OrderState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Total     float64   `json:"total"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`
	version   uint32
}

func NewOrderState(id string) *OrderState {
	return &OrderState{
		ID:     id,
		Status: "PENDING",
	}
}

func (s *OrderState) Version() uint32 {
	return s.version
}

func (s *OrderState) Apply(eventType string, payload []byte, timestamp time.Time) error {
	switch eventType {
	case "OrderCreated":
		var data struct {
			Total    float64 `json:"total"`
			Currency string  `json:"currency"`
		}
		if err := json.Unmarshal(payload, &data); err != nil {
			return fmt.Errorf("domain: malformed OrderCreated payload: %w", err)
		}
		s.Total = data.Total
		s.Currency = data.Currency
		s.Status = "CREATED"

	case "OrderPaid":
		s.Status = "PAID"

	case "OrderShipped":
		s.Status = "SHIPPED"

	default:
		return fmt.Errorf("domain: unrecognized event type %q for OrderState", eventType)
	}

	s.version++
	s.UpdatedAt = timestamp
	return nil
}
