package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/simpul/hr-backend/internal/domain"
	"gorm.io/gorm"
)

type Store struct{ DB *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{DB: db} }

func (s *Store) Tenant(ctx context.Context, organizationID string) *gorm.DB {
	return s.DB.WithContext(ctx).Where("organization_id = ?", organizationID)
}

func (s *Store) Transaction(ctx context.Context, organizationID string, fn func(*gorm.DB) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if organizationID != "" {
			if err := tx.Exec("SELECT set_config('app.organization_id', ?, true)", organizationID).Error; err != nil {
				return fmt.Errorf("set tenant context: %w", err)
			}
		}
		return fn(tx)
	})
}

func Audit(tx *gorm.DB, organizationID, actorID, requestID, action, entityType, entityID string, before, after any) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	log := domain.AuditLog{
		OrganizationID: organizationID,
		ActorUserID:    actor,
		RequestID:      requestID,
		Action:         action,
		EntityType:     entityType,
		EntityID:       entityID,
		Before:         nullJSON(beforeJSON),
		After:          nullJSON(afterJSON),
	}
	return tx.Create(&log).Error
}

func AddOutbox(tx *gorm.DB, organizationID, topic string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := domain.OutboxEvent{
		OrganizationID: organizationID,
		Topic:          topic,
		Payload:        raw,
		AvailableAt:    time.Now().UTC(),
	}
	return tx.Create(&event).Error
}

func nullJSON(value []byte) json.RawMessage {
	if string(value) == "null" {
		return nil
	}
	return value
}
