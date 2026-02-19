package hfpoc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	HF struct {
		BaseURL  string `yaml:"base_url"`
		TokenEnv string `yaml:"token_env"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"hf"`

	Assess struct {
		UpstreamRepoID string `yaml:"upstream_repo_id"`
		RulesFile      string `yaml:"rules_file"`
	} `yaml:"assess"`

	Clone struct {
		UpstreamRepoID string   `yaml:"upstream_repo_id"`
		UpstreamSHA    string   `yaml:"upstream_sha"`
		Org            string   `yaml:"org"`
		OrgRepo        string   `yaml:"org_repo"`
		Private        bool     `yaml:"private"`
		AllowedFiles   []string `yaml:"allowed_files"`
		RequireGitLFS  bool     `yaml:"require_git_lfs"`
	} `yaml:"clone"`

	Verify struct {
		OrgRepoID        string            `yaml:"org_repo_id"`
		OrgOnlyNamespace string            `yaml:"org_only_namespace"`
		Revision         string            `yaml:"revision"`
		AllowedFiles     []string          `yaml:"allowed_files"`
		SHA256ByFile     map[string]string `yaml:"sha256_by_file"`
	} `yaml:"verify"`

	State struct {
		File string `yaml:"file"`
	} `yaml:"state"`
}

type State struct {
	OrgSHA    string `yaml:"org_sha"`
	UpdatedAt string `yaml:"updated_at"`
}

func ResolveConfigPath(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	if env := strings.TrimSpace(os.Getenv("HF_POC_CONFIG")); env != "" {
		return env
	}
	for _, candidate := range []string{"config/hf_poc.yaml", "hf_poc.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "config/hf_poc.yaml"
}

func ReadAppConfig(path string) (AppConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read app config %q: %w", path, err)
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse app config %q: %w", path, err)
	}
	return cfg, nil
}

func ReadAssessRules(path string) (AssessRules, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return AssessRules{}, fmt.Errorf("read assess rules %q: %w", path, err)
	}
	var rules AssessRules
	if err := yaml.Unmarshal(b, &rules); err != nil {
		return AssessRules{}, fmt.Errorf("parse assess rules %q: %w", path, err)
	}
	return rules, nil
}

func ReadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := yaml.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("parse state %q: %w", path, err)
	}
	return s, nil
}

func WriteState(path string, s State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir for %q: %w", path, err)
	}
	b, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state %q: %w", path, err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write state %q: %w", path, err)
	}
	return nil
}
