package endoflife

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Test policy for validation tests
var testPolicy = Policy{
	Version: "1.0.0",
	Updated: "2025-05-28",
	Runtimes: []PolicyRuntime{
		{
			Name:           "python",
			VersionPattern: "major_minor",
			Versions: []PolicyVersion{
				{
					Version:     "3.13",
					Supported:   true,
					Recommended: true,
					LTS:         false,
				},
				{
					Version:     "3.12",
					Supported:   true,
					Recommended: false,
					LTS:         false,
				},
			},
		},
		{
			Name:           "nodejs",
			VersionPattern: "major",
			Versions: []PolicyVersion{
				{
					Version:     "22",
					Supported:   true,
					Recommended: true,
					LTS:         true,
				},
				{
					Version:     "20",
					Supported:   true,
					Recommended: false,
					LTS:         true,
				},
			},
		},
	},
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.BaseURL != DefaultBaseURL {
		t.Errorf("Expected BaseURL %s, got %s", DefaultBaseURL, config.BaseURL)
	}

	if config.UserAgent != DefaultUserAgent {
		t.Errorf("Expected UserAgent %s, got %s", DefaultUserAgent, config.UserAgent)
	}

	if config.Timeout != DefaultTimeout {
		t.Errorf("Expected Timeout %v, got %v", DefaultTimeout, config.Timeout)
	}

	if config.HTTPClient == nil {
		t.Error("Expected HTTPClient to be set")
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   Config
	}{
		{
			name:   "empty config uses defaults",
			config: Config{},
			want: Config{
				BaseURL:   DefaultBaseURL,
				UserAgent: DefaultUserAgent,
				Timeout:   DefaultTimeout,
			},
		},
		{
			name: "partial config fills defaults",
			config: Config{
				BaseURL: "https://custom.api.com",
			},
			want: Config{
				BaseURL:   "https://custom.api.com",
				UserAgent: DefaultUserAgent,
				Timeout:   DefaultTimeout,
			},
		},
		{
			name: "full config preserved",
			config: Config{
				BaseURL:   "https://custom.api.com",
				UserAgent: "custom-agent",
				Timeout:   60 * time.Second,
			},
			want: Config{
				BaseURL:   "https://custom.api.com",
				UserAgent: "custom-agent",
				Timeout:   60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config).(*client)

			if client.config.BaseURL != tt.want.BaseURL {
				t.Errorf("Expected BaseURL %s, got %s", tt.want.BaseURL, client.config.BaseURL)
			}

			if client.config.UserAgent != tt.want.UserAgent {
				t.Errorf("Expected UserAgent %s, got %s", tt.want.UserAgent, client.config.UserAgent)
			}

			if client.config.Timeout != tt.want.Timeout {
				t.Errorf("Expected Timeout %v, got %v", tt.want.Timeout, client.config.Timeout)
			}

			if client.config.HTTPClient == nil {
				t.Error("Expected HTTPClient to be set")
			}

			if client.validator == nil {
				t.Error("Expected validator to be set")
			}
		})
	}
}

func TestClient_GetProductInfo(t *testing.T) {
	tests := []struct {
		name           string
		product        string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantErr        bool
		errType        error
		validateResult func(t *testing.T, productInfo *ProductInfo)
	}{
		{
			name:    "successful python request",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products/python" {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				// Generate a minimal but valid response
				response := generateMinimalProductResponse("python", []map[string]interface{}{
					{
						"name":         "3.13",
						"isEoas":       false,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.13.3"},
					},
					{
						"name":         "3.12",
						"isEoas":       true,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.12.10"},
					},
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			},
			wantErr: false,
			validateResult: func(t *testing.T, productInfo *ProductInfo) {
				if productInfo.Result.Name != "python" {
					t.Errorf("Expected product name python, got %s", productInfo.Result.Name)
				}
				if len(productInfo.Result.Releases) < 1 {
					t.Error("Expected at least one release")
				}
			},
		},
		{
			name:    "successful nodejs request",
			product: "nodejs",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products/nodejs" {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				response := generateMinimalProductResponse("nodejs", []map[string]interface{}{
					{
						"name":         "22",
						"isEoas":       false,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "22.16.0"},
					},
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			},
			wantErr: false,
			validateResult: func(t *testing.T, productInfo *ProductInfo) {
				if productInfo.Result.Name != "nodejs" {
					t.Errorf("Expected product name nodejs, got %s", productInfo.Result.Name)
				}
			},
		},
		{
			name:    "empty product name",
			product: "",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				// This should never be called for empty product
				w.WriteHeader(http.StatusBadRequest)
			},
			wantErr: true,
			errType: ErrInvalidResponse,
		},
		{
			name:    "product not found",
			product: "nonexistent",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error": "not found"}`))
			},
			wantErr: true,
			errType: ErrProductNotFound,
		},
		{
			name:    "server error",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error": "internal server error"}`))
			},
			wantErr: true,
			errType: ErrNetworkError,
		},
		{
			name:    "invalid JSON response",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{invalid json`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			// Create client with test server
			client := NewClient(Config{
				BaseURL: server.URL + "/v1",
			})

			// Make request
			ctx := context.Background()
			productInfo, err := client.GetProductInfo(ctx, tt.product)

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
					return
				}
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("Expected error type %v, got %v", tt.errType, err)
				}
				return
			}

			// Check success case
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if productInfo == nil {
				t.Error("Expected productInfo, got nil")
				return
			}

			// Run custom validation if provided
			if tt.validateResult != nil {
				tt.validateResult(t, productInfo)
			}
		})
	}
}

func TestClient_GetSupportedVersions(t *testing.T) {
	tests := []struct {
		name           string
		runtime        PolicyRuntime
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantErr        bool
		validateResult func(t *testing.T, versions []VersionInfo)
	}{
		{
			name: "python runtime with supported versions",
			runtime: PolicyRuntime{
				Name:           "python",
				VersionPattern: "major_minor",
				Versions: []PolicyVersion{
					{Version: "3.13", Supported: true, Recommended: true},
					{Version: "3.12", Supported: true, Recommended: false},
					{Version: "3.11", Supported: false, Recommended: false}, // Not supported
				},
			},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products/python" {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				response := generateMinimalProductResponse("python", []map[string]interface{}{
					{
						"name":         "3.13",
						"isEoas":       false,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.13.3"},
					},
					{
						"name":         "3.12",
						"isEoas":       true,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.12.10"},
					},
					{
						"name":         "3.11",
						"isEoas":       true,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.11.12"},
					},
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			},
			wantErr: false,
			validateResult: func(t *testing.T, versions []VersionInfo) {
				// Should only have 2 versions since 3.11 is not supported in policy
				if len(versions) != 2 {
					t.Errorf("Expected 2 supported versions, got %d", len(versions))
				}

				// Check that all returned versions are supported
				for _, v := range versions {
					if !v.IsSupported {
						t.Errorf("Version %s should be supported", v.Version)
					}
					if v.Version == "" {
						t.Error("Version should not be empty")
					}
					if v.RuntimeName != "python" {
						t.Errorf("Expected runtime name python, got %s", v.RuntimeName)
					}
				}

				// Check for specific versions
				versionMap := make(map[string]VersionInfo)
				for _, v := range versions {
					versionMap[v.Version] = v
				}

				if v313, exists := versionMap["3.13"]; exists {
					if v313.IsSecurityOnly() {
						t.Error("Python 3.13 should not be security-only")
					}
					if v313.IsRecommended != true {
						t.Error("Python 3.13 should be recommended")
					}
				}

				if v312, exists := versionMap["3.12"]; exists {
					if !v312.IsSecurityOnly() {
						t.Error("Python 3.12 should be security-only")
					}
				}
			},
		},
		{
			name: "nodejs runtime with supported versions",
			runtime: PolicyRuntime{
				Name:           "nodejs",
				VersionPattern: "major",
				Versions: []PolicyVersion{
					{Version: "22", Supported: true, Recommended: true},
					{Version: "20", Supported: true, Recommended: false},
				},
			},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products/nodejs" {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				response := generateMinimalProductResponse("nodejs", []map[string]interface{}{
					{
						"name":         "22",
						"isEoas":       false,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "22.16.0"},
					},
					{
						"name":         "20",
						"isEoas":       true,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "20.15.1"},
					},
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			},
			wantErr: false,
			validateResult: func(t *testing.T, versions []VersionInfo) {
				if len(versions) != 2 {
					t.Errorf("Expected 2 supported versions, got %d", len(versions))
				}

				for _, v := range versions {
					if v.RuntimeName != "nodejs" {
						t.Errorf("Expected runtime name nodejs, got %s", v.RuntimeName)
					}
				}
			},
		},
		{
			name: "runtime with no supported versions",
			runtime: PolicyRuntime{
				Name:           "python",
				VersionPattern: "major_minor",
				Versions: []PolicyVersion{
					{Version: "3.11", Supported: false}, // Not supported
				},
			},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				response := generateMinimalProductResponse("python", []map[string]interface{}{
					{
						"name":         "3.11",
						"isEoas":       true,
						"isEol":        false,
						"isMaintained": true,
						"latest":       map[string]string{"name": "3.11.12"},
					},
				})

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(response))
			},
			wantErr: false,
			validateResult: func(t *testing.T, versions []VersionInfo) {
				if len(versions) != 0 {
					t.Errorf("Expected 0 supported versions, got %d", len(versions))
				}
			},
		},
		{
			name: "nonexistent runtime",
			runtime: PolicyRuntime{
				Name:           "nonexistent",
				VersionPattern: "major_minor",
				Versions: []PolicyVersion{
					{Version: "1.0", Supported: true},
				},
			},
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error": "not found"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			// Create client with test server
			client := NewClient(Config{
				BaseURL: server.URL + "/v1",
			})

			// Make request
			ctx := context.Background()
			versions, err := client.GetSupportedVersions(ctx, tt.runtime)

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			// Check success case
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Run custom validation if provided
			if tt.validateResult != nil {
				tt.validateResult(t, versions)
			}
		})
	}
}

func TestClient_ValidatePolicy(t *testing.T) {
	client := NewClient(DefaultConfig())

	tests := []struct {
		name    string
		policy  *Policy
		wantErr bool
		errType error
	}{
		{
			name:    "valid policy",
			policy:  &testPolicy,
			wantErr: false,
		},
		{
			name:    "nil policy",
			policy:  nil,
			wantErr: true,
		},
		{
			name: "empty version",
			policy: &Policy{
				Version:  "",
				Runtimes: []PolicyRuntime{testPolicy.Runtimes[0]},
			},
			wantErr: true,
		},
		{
			name: "no runtimes",
			policy: &Policy{
				Version:  "1.0.0",
				Runtimes: []PolicyRuntime{},
			},
			wantErr: true,
		},
		{
			name: "runtime with empty name",
			policy: &Policy{
				Version: "1.0.0",
				Runtimes: []PolicyRuntime{
					{
						Name:           "",
						VersionPattern: "major_minor",
						Versions:       []PolicyVersion{{Version: "1.0", Supported: true}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "runtime with invalid version pattern",
			policy: &Policy{
				Version: "1.0.0",
				Runtimes: []PolicyRuntime{
					{
						Name:           "test",
						VersionPattern: "invalid",
						Versions:       []PolicyVersion{{Version: "1.0", Supported: true}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "runtime with no versions",
			policy: &Policy{
				Version: "1.0.0",
				Runtimes: []PolicyRuntime{
					{
						Name:           "test",
						VersionPattern: "major_minor",
						Versions:       []PolicyVersion{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "runtime with empty version",
			policy: &Policy{
				Version: "1.0.0",
				Runtimes: []PolicyRuntime{
					{
						Name:           "test",
						VersionPattern: "major_minor",
						Versions:       []PolicyVersion{{Version: "", Supported: true}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "runtime with invalid semver",
			policy: &Policy{
				Version: "1.0.0",
				Runtimes: []PolicyRuntime{
					{
						Name:           "test",
						VersionPattern: "major_minor",
						Versions:       []PolicyVersion{{Version: "invalid.version", Supported: true}},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ValidatePolicy(tt.policy)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				// Check if it's a policy validation error
				var policyErr ErrPolicyValidation
				if !errors.As(err, &policyErr) {
					t.Errorf("Expected ErrPolicyValidation, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClient_EnrichVersionInfo(t *testing.T) {
	client := NewClient(DefaultConfig())

	tests := []struct {
		name          string
		runtime       PolicyRuntime
		policyVersion PolicyVersion
		wantErr       bool
	}{
		{
			name:          "python version with major_minor pattern",
			runtime:       testPolicy.Runtimes[0],
			policyVersion: testPolicy.Runtimes[0].Versions[0],
			wantErr:       false,
		},
		{
			name:          "nodejs version with major pattern",
			runtime:       testPolicy.Runtimes[1],
			policyVersion: testPolicy.Runtimes[1].Versions[0],
			wantErr:       false,
		},
		{
			name: "version with EOL date",
			runtime: PolicyRuntime{
				Name:           "test",
				VersionPattern: "major_minor",
			},
			policyVersion: PolicyVersion{
				Version:   "1.0",
				Supported: true,
				EOL:       "2025-12-31",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			versionInfo, err := client.EnrichVersionInfo(ctx, tt.runtime, tt.policyVersion)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if versionInfo == nil {
				t.Error("Expected versionInfo, got nil")
				return
			}

			if versionInfo.Version != tt.policyVersion.Version {
				t.Errorf("Expected version %s, got %s", tt.policyVersion.Version, versionInfo.Version)
			}

			if versionInfo.IsSupported != tt.policyVersion.Supported {
				t.Errorf("Expected supported %v, got %v", tt.policyVersion.Supported, versionInfo.IsSupported)
			}

			if versionInfo.IsRecommended != tt.policyVersion.Recommended {
				t.Errorf("Expected recommended %v, got %v", tt.policyVersion.Recommended, versionInfo.IsRecommended)
			}

			if versionInfo.IsLTS != tt.policyVersion.LTS {
				t.Errorf("Expected LTS %v, got %v", tt.policyVersion.LTS, versionInfo.IsLTS)
			}

			if versionInfo.RuntimeName != tt.runtime.Name {
				t.Errorf("Expected runtime name %s, got %s", tt.runtime.Name, versionInfo.RuntimeName)
			}

			if tt.policyVersion.EOL != "" && versionInfo.EOLDate != tt.policyVersion.EOL {
				t.Errorf("Expected EOL date %s, got %s", tt.policyVersion.EOL, versionInfo.EOLDate)
			}
		})
	}
}

func TestErrAPIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ErrAPIError
		want string
	}{
		{
			name: "error with product",
			err: ErrAPIError{
				StatusCode: 404,
				Message:    "Not Found",
				Product:    "python",
			},
			want: "API error for product python: 404 Not Found",
		},
		{
			name: "error without product",
			err: ErrAPIError{
				StatusCode: 500,
				Message:    "Internal Server Error",
			},
			want: "API error: 500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("ErrAPIError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrAPIError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    ErrAPIError
		target error
		want   bool
	}{
		{
			name: "404 is ErrProductNotFound",
			err: ErrAPIError{
				StatusCode: 404,
			},
			target: ErrProductNotFound,
			want:   true,
		},
		{
			name: "400 is ErrInvalidResponse",
			err: ErrAPIError{
				StatusCode: 400,
			},
			target: ErrInvalidResponse,
			want:   true,
		},
		{
			name: "500 is ErrNetworkError",
			err: ErrAPIError{
				StatusCode: 500,
			},
			target: ErrNetworkError,
			want:   true,
		},
		{
			name: "200 is not an error",
			err: ErrAPIError{
				StatusCode: 200,
			},
			target: ErrProductNotFound,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Is(tt.target); got != tt.want {
				t.Errorf("ErrAPIError.Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrPolicyValidation_Error(t *testing.T) {
	err := ErrPolicyValidation{
		Field:   "version",
		Value:   "invalid",
		Reason:  "not semver",
		Runtime: "python",
	}

	want := "policy validation failed for python.version=invalid: not semver"
	if got := err.Error(); got != want {
		t.Errorf("ErrPolicyValidation.Error() = %v, want %v", got, want)
	}
}

func TestJSONPolicyLoader_LoadPolicy(t *testing.T) {
	loader := NewJSONPolicyLoader()

	tests := []struct {
		name     string
		filePath string
		fileData string
		wantErr  bool
	}{
		{
			name:     "empty file path",
			filePath: "",
			wantErr:  true,
		},
		{
			name:     "valid policy file",
			filePath: "test-policy.json",
			fileData: mustMarshalJSON(testPolicy),
			wantErr:  false,
		},
		{
			name:     "invalid JSON",
			filePath: "invalid.json",
			fileData: "invalid json",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up file reader for testing
			originalReader := readFile
			defer func() { readFile = originalReader }()

			readFile = func(filename string) ([]byte, error) {
				if filename != tt.filePath {
					return nil, fmt.Errorf("unexpected file path: %s", filename)
				}
				if tt.fileData == "" {
					return nil, fmt.Errorf("file not found")
				}
				return []byte(tt.fileData), nil
			}

			policy, err := loader.LoadPolicy(tt.filePath)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if policy == nil {
				t.Error("Expected policy, got nil")
				return
			}

			if policy.Version != testPolicy.Version {
				t.Errorf("Expected version %s, got %s", testPolicy.Version, policy.Version)
			}

			if len(policy.Runtimes) != len(testPolicy.Runtimes) {
				t.Errorf("Expected %d runtimes, got %d", len(testPolicy.Runtimes), len(policy.Runtimes))
			}
		})
	}
}

func TestSetFileReader(t *testing.T) {
	originalReader := readFile
	defer func() { readFile = originalReader }()

	testReader := func(filename string) ([]byte, error) {
		return []byte("test data"), nil
	}

	SetFileReader(testReader)

	data, err := readFile("test.txt")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if string(data) != "test data" {
		t.Errorf("Expected 'test data', got %s", string(data))
	}
}

// Integration test with real HTTP server
func TestClient_Integration(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "python") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(generateRealisticPythonResponse()))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create client with test server
	config := DefaultConfig()
	config.BaseURL = server.URL + "/v1"
	client := NewClient(config)

	// Test GetProductInfo
	ctx := context.Background()
	productInfo, err := client.GetProductInfo(ctx, "python")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if productInfo.Result.Name != "python" {
		t.Errorf("Expected product name python, got %s", productInfo.Result.Name)
	}

	// Test GetSupportedVersions
	runtime := testPolicy.Runtimes[0] // python runtime
	versions, err := client.GetSupportedVersions(ctx, runtime)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if len(versions) == 0 {
		t.Error("Expected supported versions, got empty slice")
	}

	// Test ValidatePolicy
	err = client.ValidatePolicy(&testPolicy)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// Helper function to marshal JSON for tests
func mustMarshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestJSONPolicyLoader_LoadArrayPolicy(t *testing.T) {
	loader := NewJSONPolicyLoader()

	// Create test data inline instead of using removed global variables
	pythonVersions := []PolicyVersion{
		{Version: "3.13", Supported: true, Recommended: true, LTS: false},
		{Version: "3.12", Supported: true, Recommended: false, LTS: false},
		{Version: "3.11", Supported: false, Recommended: false, LTS: false},
	}

	nodejsVersions := []PolicyVersion{
		{Version: "22", Supported: true, Recommended: true, LTS: true},
		{Version: "20", Supported: true, Recommended: false, LTS: true},
		{Version: "18", Supported: false, Recommended: false, LTS: true},
	}

	tests := []struct {
		name           string
		filePath       string
		runtimeName    string
		versionPattern string
		fileData       string
		wantErr        bool
		expectedCount  int
	}{
		{
			name:           "empty file path",
			filePath:       "",
			runtimeName:    "python",
			versionPattern: "major_minor",
			wantErr:        true,
		},
		{
			name:           "empty runtime name",
			filePath:       "test.json",
			runtimeName:    "",
			versionPattern: "major_minor",
			wantErr:        true,
		},
		{
			name:           "valid python array policy",
			filePath:       "python-policy.json",
			runtimeName:    "python",
			versionPattern: "major_minor",
			fileData:       mustMarshalJSON(pythonVersions),
			wantErr:        false,
			expectedCount:  3,
		},
		{
			name:           "valid nodejs array policy",
			filePath:       "nodejs-policy.json",
			runtimeName:    "nodejs",
			versionPattern: "major",
			fileData:       mustMarshalJSON(nodejsVersions),
			wantErr:        false,
			expectedCount:  3,
		},
		{
			name:           "invalid JSON",
			filePath:       "invalid.json",
			runtimeName:    "python",
			versionPattern: "major_minor",
			fileData:       "invalid json",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up file reader for testing
			originalReader := readFile
			defer func() { readFile = originalReader }()

			readFile = func(filename string) ([]byte, error) {
				if filename != tt.filePath {
					return nil, fmt.Errorf("unexpected file path: %s", filename)
				}
				if tt.fileData == "" {
					return nil, fmt.Errorf("file not found")
				}
				return []byte(tt.fileData), nil
			}

			policy, err := loader.LoadArrayPolicy(tt.filePath, tt.runtimeName, tt.versionPattern)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if policy == nil {
				t.Error("Expected policy, got nil")
				return
			}

			if len(policy.Runtimes) != 1 {
				t.Errorf("Expected 1 runtime, got %d", len(policy.Runtimes))
				return
			}

			runtime := policy.Runtimes[0]
			if runtime.Name != tt.runtimeName {
				t.Errorf("Expected runtime name %s, got %s", tt.runtimeName, runtime.Name)
			}

			if runtime.VersionPattern != tt.versionPattern {
				t.Errorf("Expected version pattern %s, got %s", tt.versionPattern, runtime.VersionPattern)
			}

			if len(runtime.Versions) != tt.expectedCount {
				t.Errorf("Expected %d versions, got %d", tt.expectedCount, len(runtime.Versions))
			}

			// Verify version structure - just check that versions exist and have the required field
			for _, version := range runtime.Versions {
				if version.Version == "" {
					t.Error("Expected version to be set")
				}
			}
		})
	}
}

// Test helper functions for generating realistic API responses

func generateMinimalProductResponse(productName string, releases []map[string]interface{}) string {
	releaseStrings := make([]string, 0, len(releases))

	for _, release := range releases {
		// Set defaults for required fields
		name := release["name"].(string)
		isEoas, _ := release["isEoas"].(bool)
		isEol, _ := release["isEol"].(bool)
		isMaintained, _ := release["isMaintained"].(bool)

		latest := release["latest"].(map[string]string)
		latestName := latest["name"]

		releaseStr := fmt.Sprintf(`{
			"name": "%s",
			"label": "%s",
			"releaseDate": "2023-01-01",
			"isLts": false,
			"isEoas": %t,
			"isEol": %t,
			"isMaintained": %t,
			"latest": {
				"name": "%s",
				"date": "2025-04-08",
				"link": "https://example.com/release"
			}
		}`, name, name, isEoas, isEol, isMaintained, latestName)

		releaseStrings = append(releaseStrings, releaseStr)
	}

	return fmt.Sprintf(`{
		"schema_version": "1.1.0",
		"generated_at": "2025-05-31T16:37:04+00:00",
		"last_modified": "2025-04-09T13:02:28+00:00",
		"result": {
			"name": "%s",
			"aliases": [],
			"label": "%s",
			"category": "lang",
			"tags": ["lang"],
			"releases": [%s]
		}
	}`, productName, strings.ToUpper(productName[:1])+productName[1:], strings.Join(releaseStrings, ","))
}

func generateRealisticPythonResponse() string {
	return `{
		"schema_version": "1.1.0",
		"generated_at": "2025-05-31T16:37:04+00:00",
		"last_modified": "2025-04-09T13:02:28+00:00",
		"result": {
			"name": "python",
			"aliases": [],
			"label": "Python",
			"category": "lang",
			"tags": ["lang"],
			"versionCommand": "python --version",
			"labels": {
				"eoas": "Active Support",
				"eol": "Security Support"
			},
			"releases": [
				{
					"name": "3.13",
					"label": "3.13",
					"releaseDate": "2024-10-07",
					"isLts": false,
					"isEoas": false,
					"eoasFrom": "2026-10-01",
					"isEol": false,
					"eolFrom": "2029-10-31",
					"isMaintained": true,
					"latest": {
						"name": "3.13.3",
						"date": "2025-04-08",
						"link": "https://www.python.org/downloads/release/python-3133/"
					}
				},
				{
					"name": "3.12",
					"label": "3.12",
					"releaseDate": "2023-10-02",
					"isLts": false,
					"isEoas": true,
					"eoasFrom": "2025-04-02",
					"isEol": false,
					"eolFrom": "2028-10-31",
					"isMaintained": true,
					"latest": {
						"name": "3.12.10",
						"date": "2025-04-08",
						"link": "https://www.python.org/downloads/release/python-31210/"
					}
				},
				{
					"name": "3.11",
					"label": "3.11",
					"releaseDate": "2022-10-24",
					"isLts": false,
					"isEoas": true,
					"eoasFrom": "2024-04-01",
					"isEol": false,
					"eolFrom": "2027-10-31",
					"isMaintained": true,
					"latest": {
						"name": "3.11.12",
						"date": "2025-04-08",
						"link": "https://www.python.org/downloads/release/python-31112/"
					}
				},
				{
					"name": "3.10",
					"label": "3.10",
					"releaseDate": "2021-10-04",
					"isLts": false,
					"isEoas": true,
					"eoasFrom": "2023-04-05",
					"isEol": false,
					"eolFrom": "2026-10-31",
					"isMaintained": true,
					"latest": {
						"name": "3.10.17",
						"date": "2025-04-08",
						"link": "https://www.python.org/downloads/release/python-31017/"
					}
				},
				{
					"name": "3.8",
					"label": "3.8",
					"releaseDate": "2019-10-14",
					"isLts": false,
					"isEoas": true,
					"eoasFrom": "2021-05-03",
					"isEol": true,
					"eolFrom": "2024-10-07",
					"isMaintained": false,
					"latest": {
						"name": "3.8.20",
						"date": "2024-09-06",
						"link": "https://www.python.org/downloads/release/python-3820/"
					}
				}
			]
		}
	}`
}

func TestVersionInfo_SecurityOnlyMethods(t *testing.T) {
	tests := []struct {
		name                     string
		versionInfo              VersionInfo
		expectedIsSecurityOnly   bool
		expectedShouldSkipBinary bool
		expectedLifecycleStatus  string
	}{
		{
			name: "Active Support Version (3.13)",
			versionInfo: VersionInfo{
				Version:      "3.13",
				IsEOAS:       false,
				IsEOL:        false,
				IsMaintained: true,
			},
			expectedIsSecurityOnly:   false,
			expectedShouldSkipBinary: false,
			expectedLifecycleStatus:  "Active Support",
		},
		{
			name: "Security Only Version (3.10)",
			versionInfo: VersionInfo{
				Version:      "3.10",
				IsEOAS:       true,
				IsEOL:        false,
				IsMaintained: true,
			},
			expectedIsSecurityOnly:   true,
			expectedShouldSkipBinary: true,
			expectedLifecycleStatus:  "Security Support Only",
		},
		{
			name: "End of Life Version (3.8)",
			versionInfo: VersionInfo{
				Version:      "3.8",
				IsEOAS:       true,
				IsEOL:        true,
				IsMaintained: false,
			},
			expectedIsSecurityOnly:   false,
			expectedShouldSkipBinary: false,
			expectedLifecycleStatus:  "End of Life",
		},
		{
			name: "Edge Case - EOAS but not maintained",
			versionInfo: VersionInfo{
				Version:      "2.7",
				IsEOAS:       true,
				IsEOL:        false,
				IsMaintained: false,
			},
			expectedIsSecurityOnly:   false,
			expectedShouldSkipBinary: false,
			expectedLifecycleStatus:  "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test IsSecurityOnly
			if got := tt.versionInfo.IsSecurityOnly(); got != tt.expectedIsSecurityOnly {
				t.Errorf("IsSecurityOnly() = %v, want %v", got, tt.expectedIsSecurityOnly)
			}

			// Test ShouldSkipBinaryDownloads
			if got := tt.versionInfo.ShouldSkipBinaryDownloads(); got != tt.expectedShouldSkipBinary {
				t.Errorf("ShouldSkipBinaryDownloads() = %v, want %v", got, tt.expectedShouldSkipBinary)
			}

			// Test GetLifecycleStatus
			if got := tt.versionInfo.GetLifecycleStatus(); got != tt.expectedLifecycleStatus {
				t.Errorf("GetLifecycleStatus() = %v, want %v", got, tt.expectedLifecycleStatus)
			}
		})
	}
}

func TestClient_GetSupportedVersions_SecurityOnlyDetection(t *testing.T) {
	// Create test server with realistic Python API response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/products/python" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(generateRealisticPythonResponse()))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create client with test server
	client := NewClient(Config{
		BaseURL: server.URL + "/v1",
	})

	// Create test policy runtime
	runtime := PolicyRuntime{
		Name:           "python",
		VersionPattern: "major_minor",
		Versions: []PolicyVersion{
			{Version: "3.13", Supported: true, Recommended: true},
			{Version: "3.12", Supported: true, Recommended: false},
			{Version: "3.11", Supported: true, Recommended: false},
			{Version: "3.10", Supported: true, Recommended: false},
			{Version: "3.8", Supported: true, Recommended: false},
		},
	}

	// Get supported versions
	ctx := context.Background()
	versions, err := client.GetSupportedVersions(ctx, runtime)
	if err != nil {
		t.Fatalf("GetSupportedVersions() error = %v", err)
	}

	// Verify we got the expected versions
	if len(versions) != 5 {
		t.Fatalf("Expected 5 versions, got %d", len(versions))
	}

	// Test specific version behaviors
	versionMap := make(map[string]VersionInfo)
	for _, v := range versions {
		versionMap[v.Version] = v
	}

	tests := []struct {
		version                  string
		expectedIsSecurityOnly   bool
		expectedShouldSkipBinary bool
		expectedLifecycleStatus  string
		expectedIsEOAS           bool
		expectedIsEOL            bool
		expectedIsMaintained     bool
	}{
		{
			version:                  "3.13",
			expectedIsSecurityOnly:   false,
			expectedShouldSkipBinary: false,
			expectedLifecycleStatus:  "Active Support",
			expectedIsEOAS:           false,
			expectedIsEOL:            false,
			expectedIsMaintained:     true,
		},
		{
			version:                  "3.12",
			expectedIsSecurityOnly:   true,
			expectedShouldSkipBinary: true,
			expectedLifecycleStatus:  "Security Support Only",
			expectedIsEOAS:           true,
			expectedIsEOL:            false,
			expectedIsMaintained:     true,
		},
		{
			version:                  "3.11",
			expectedIsSecurityOnly:   true,
			expectedShouldSkipBinary: true,
			expectedLifecycleStatus:  "Security Support Only",
			expectedIsEOAS:           true,
			expectedIsEOL:            false,
			expectedIsMaintained:     true,
		},
		{
			version:                  "3.10",
			expectedIsSecurityOnly:   true,
			expectedShouldSkipBinary: true,
			expectedLifecycleStatus:  "Security Support Only",
			expectedIsEOAS:           true,
			expectedIsEOL:            false,
			expectedIsMaintained:     true,
		},
		{
			version:                  "3.8",
			expectedIsSecurityOnly:   false,
			expectedShouldSkipBinary: false,
			expectedLifecycleStatus:  "End of Life",
			expectedIsEOAS:           true,
			expectedIsEOL:            true,
			expectedIsMaintained:     false,
		},
	}

	for _, tt := range tests {
		t.Run("Version_"+tt.version, func(t *testing.T) {
			versionInfo, exists := versionMap[tt.version]
			if !exists {
				t.Fatalf("Version %s not found in results", tt.version)
			}

			// Test lifecycle flags
			if versionInfo.IsEOAS != tt.expectedIsEOAS {
				t.Errorf("Version %s: IsEOAS = %v, want %v", tt.version, versionInfo.IsEOAS, tt.expectedIsEOAS)
			}
			if versionInfo.IsEOL != tt.expectedIsEOL {
				t.Errorf("Version %s: IsEOL = %v, want %v", tt.version, versionInfo.IsEOL, tt.expectedIsEOL)
			}
			if versionInfo.IsMaintained != tt.expectedIsMaintained {
				t.Errorf("Version %s: IsMaintained = %v, want %v", tt.version, versionInfo.IsMaintained, tt.expectedIsMaintained)
			}

			// Test derived methods
			if versionInfo.IsSecurityOnly() != tt.expectedIsSecurityOnly {
				t.Errorf("Version %s: IsSecurityOnly() = %v, want %v", tt.version, versionInfo.IsSecurityOnly(), tt.expectedIsSecurityOnly)
			}
			if versionInfo.ShouldSkipBinaryDownloads() != tt.expectedShouldSkipBinary {
				t.Errorf("Version %s: ShouldSkipBinaryDownloads() = %v, want %v", tt.version, versionInfo.ShouldSkipBinaryDownloads(), tt.expectedShouldSkipBinary)
			}
			if versionInfo.GetLifecycleStatus() != tt.expectedLifecycleStatus {
				t.Errorf("Version %s: GetLifecycleStatus() = %v, want %v", tt.version, versionInfo.GetLifecycleStatus(), tt.expectedLifecycleStatus)
			}

			// Verify latest patch is populated
			if versionInfo.LatestPatch == "" {
				t.Errorf("Version %s: LatestPatch should not be empty", tt.version)
			}
		})
	}
}

func TestClient_GetProductInfo_WithHTTPTest(t *testing.T) {
	tests := []struct {
		name           string
		product        string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		wantErr        bool
		errType        error
	}{
		{
			name:    "successful python request with realistic data",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/products/python" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(generateRealisticPythonResponse()))
			},
			wantErr: false,
		},
		{
			name:    "product not found",
			product: "nonexistent",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error": "not found"}`))
			},
			wantErr: true,
			errType: ErrProductNotFound,
		},
		{
			name:    "server error",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error": "internal server error"}`))
			},
			wantErr: true,
			errType: ErrNetworkError,
		},
		{
			name:    "invalid json response",
			product: "python",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{invalid json`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			// Create client
			client := NewClient(Config{
				BaseURL: server.URL + "/v1",
			})

			// Make request
			ctx := context.Background()
			productInfo, err := client.GetProductInfo(ctx, tt.product)

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetProductInfo() expected error, got nil")
					return
				}
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("GetProductInfo() error = %v, want type %v", err, tt.errType)
				}
				return
			}

			// Check success case
			if err != nil {
				t.Errorf("GetProductInfo() unexpected error = %v", err)
				return
			}

			if productInfo == nil {
				t.Error("GetProductInfo() returned nil productInfo")
				return
			}

			// Verify the response structure
			if productInfo.Result.Name != tt.product {
				t.Errorf("GetProductInfo() product name = %v, want %v", productInfo.Result.Name, tt.product)
			}

			// For Python, verify we have security-only releases
			if tt.product == "python" {
				securityOnlyCount := 0
				activeCount := 0
				eolCount := 0

				for _, release := range productInfo.Result.Releases {
					if release.IsEOAS && !release.IsEOL && release.IsMaintained {
						securityOnlyCount++
					} else if !release.IsEOAS && !release.IsEOL && release.IsMaintained {
						activeCount++
					} else if release.IsEOL {
						eolCount++
					}
				}

				if securityOnlyCount == 0 {
					t.Error("Expected at least one security-only release for Python")
				}
				if activeCount == 0 {
					t.Error("Expected at least one active support release for Python")
				}
				if eolCount == 0 {
					t.Error("Expected at least one EOL release for Python")
				}
			}
		})
	}
}
