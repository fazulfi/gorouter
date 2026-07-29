// Package provider provides domain types for AI service providers.
package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProviderType enumerates supported AI provider backends.
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderAzure     ProviderType = "azure"
	ProviderCustom    ProviderType = "custom"
)

// Provider represents an AI service provider configuration.
type Provider struct {
	ID              uuid.UUID
	Name            string
	Type            ProviderType
	BaseURL         string
	APIKeyEncrypted *string
	Config          json.RawMessage
	IsEnabled       bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ProviderConfig holds typed configuration for a provider type.
type ProviderConfig struct {
	ID           uuid.UUID
	ProviderID   uuid.UUID
	ConfigType   string
	ConfigValue  json.RawMessage
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ProviderRepository defines persistence operations for providers.
type ProviderRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*Provider, error)
	FindByType(ctx context.Context, ptype ProviderType) ([]Provider, error)
	List(ctx context.Context) ([]Provider, error)
	Create(ctx context.Context, provider *Provider) error
	Update(ctx context.Context, provider *Provider) error
	Delete(ctx context.Context, id uuid.UUID) error
}
