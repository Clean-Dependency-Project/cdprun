package endoflife

import (
	"fmt"
	"time"
)

// ClientConfig represents configuration for creating API clients
type ClientConfig struct {
	Provider string
	BaseURL  string
	Timeout  time.Duration
	Config   map[string]interface{}
}

// ClientFactory creates API clients based on provider type
type ClientFactory interface {
	CreateClient(config ClientConfig) (Client, error)
}

// DefaultClientFactory implements ClientFactory
type DefaultClientFactory struct{}

// NewClientFactory creates a new client factory
func NewClientFactory() ClientFactory {
	return &DefaultClientFactory{}
}

// CreateClient creates an API client based on the provider configuration
func (f *DefaultClientFactory) CreateClient(clientConfig ClientConfig) (Client, error) {
	switch clientConfig.Provider {
	case "endoflife", "":
		return f.createEndOfLifeClient(clientConfig)
	case "mock":
		return f.createMockClient(clientConfig)
	case "custom":
		return f.createCustomClient(clientConfig)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", clientConfig.Provider)
	}
}

func (f *DefaultClientFactory) createEndOfLifeClient(clientConfig ClientConfig) (Client, error) {
	config := DefaultConfig()

	if clientConfig.BaseURL != "" {
		config.BaseURL = clientConfig.BaseURL
	}

	if clientConfig.Timeout > 0 {
		config.Timeout = clientConfig.Timeout
	}

	return NewClient(config), nil
}

func (f *DefaultClientFactory) createMockClient(clientConfig ClientConfig) (Client, error) {
	return NewMockClient(), nil
}

func (f *DefaultClientFactory) createCustomClient(clientConfig ClientConfig) (Client, error) {
	return nil, fmt.Errorf("custom provider not yet implemented")
}
