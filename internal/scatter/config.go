package scatter

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mesh     MeshConfig               `yaml:"mesh,omitempty"`
	Contexts map[string]ContextConfig `yaml:"contexts"`
}

type MeshConfig struct {
	Enable      bool                   `yaml:"enable"`
	Context     string                 `yaml:"context"`
	Hostname    string                 `yaml:"hostname,omitempty"`
	BindAddress string                 `yaml:"bind_address"`
	BindPort    int                    `yaml:"bind_port"`
	Headscale   map[string]interface{} `yaml:"headscale,omitempty"`
}

type ContextConfig struct {
	Env      map[string]string `yaml:"env"`
	Profiles []string          `yaml:"profiles"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Deduplicate profiles
	for name, ctxCfg := range cfg.Contexts {
		ctxCfg.Profiles = deduplicate(ctxCfg.Profiles)
		cfg.Contexts[name] = ctxCfg
	}

	return &cfg, nil
}

func deduplicate(items []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
