package endoflife

import (
	"testing"
	"time"
)

func TestNewClientFactory(t *testing.T) {
	factory := NewClientFactory()
	if factory == nil {
		t.Error("NewClientFactory() returned nil")
	}
}

func TestDefaultClientFactory_CreateClient(t *testing.T) {
	factory := NewClientFactory().(*DefaultClientFactory)

	tests := []struct {
		name        string
		config      ClientConfig
		wantErr     bool
		wantErrMsg  string
		verifyClient func(t *testing.T, client Client)
	}{
		{
			name: "endoflife provider",
			config: ClientConfig{
				Provider: "endoflife",
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client for endoflife provider")
				}
			},
		},
		{
			name: "empty provider defaults to endoflife",
			config: ClientConfig{
				Provider: "",
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client for empty provider")
				}
			},
		},
		{
			name: "mock provider",
			config: ClientConfig{
				Provider: "mock",
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client for mock provider")
				}
				// Verify it's a mock client
				_, ok := client.(*MockClient)
				if !ok {
					t.Error("Expected MockClient for mock provider")
				}
			},
		},
		{
			name: "custom provider not implemented",
			config: ClientConfig{
				Provider: "custom",
			},
			wantErr:    true,
			wantErrMsg: "custom provider not yet implemented",
		},
		{
			name: "unsupported provider",
			config: ClientConfig{
				Provider: "unsupported",
			},
			wantErr:    true,
			wantErrMsg: "unsupported provider: unsupported",
		},
		{
			name: "endoflife with custom BaseURL",
			config: ClientConfig{
				Provider: "endoflife",
				BaseURL:  "https://custom.example.com/api",
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client")
				}
				// The BaseURL is set internally, we can't easily verify it without
				// making the client's config accessible, but we can verify the client works
			},
		},
		{
			name: "endoflife with custom Timeout",
			config: ClientConfig{
				Provider: "endoflife",
				Timeout:  60 * time.Second,
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client")
				}
			},
		},
		{
			name: "endoflife with both BaseURL and Timeout",
			config: ClientConfig{
				Provider: "endoflife",
				BaseURL:  "https://custom.example.com/api",
				Timeout:  45 * time.Second,
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client")
				}
			},
		},
		{
			name: "mock provider ignores config",
			config: ClientConfig{
				Provider: "mock",
				BaseURL:  "https://should-be-ignored.com",
				Timeout:  999 * time.Second,
			},
			wantErr: false,
			verifyClient: func(t *testing.T, client Client) {
				if client == nil {
					t.Error("Expected non-nil client")
				}
				_, ok := client.(*MockClient)
				if !ok {
					t.Error("Expected MockClient")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := factory.CreateClient(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateClient() expected error, got nil")
					return
				}
				if tt.wantErrMsg != "" && err.Error() != tt.wantErrMsg {
					t.Errorf("CreateClient() error = %q, want %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("CreateClient() unexpected error: %v", err)
				return
			}
			if client == nil {
				t.Error("CreateClient() returned nil client")
				return
			}
			if tt.verifyClient != nil {
				tt.verifyClient(t, client)
			}
		})
	}
}

func TestDefaultClientFactory_CreateClient_InterfaceCompliance(t *testing.T) {
	factory := NewClientFactory().(*DefaultClientFactory)

	// Test that created clients implement the Client interface
	tests := []struct {
		name   string
		config ClientConfig
	}{
		{
			name: "endoflife client",
			config: ClientConfig{
				Provider: "endoflife",
			},
		},
		{
			name: "mock client",
			config: ClientConfig{
				Provider: "mock",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := factory.CreateClient(tt.config)
			if err != nil {
				t.Fatalf("CreateClient() error: %v", err)
			}

			// Verify client implements Client interface by checking it can be assigned
			_ = client
		})
	}
}

func TestClientConfig_ZeroValue(t *testing.T) {
	// Test that zero value ClientConfig works (defaults to endoflife)
	factory := NewClientFactory().(*DefaultClientFactory)
	config := ClientConfig{}

	client, err := factory.CreateClient(config)
	if err != nil {
		t.Errorf("CreateClient() with zero value config error: %v", err)
	}
	if client == nil {
		t.Error("CreateClient() with zero value config returned nil")
	}
}

