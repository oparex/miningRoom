package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Machine struct {
	Name string `yaml:"name"`
	IP   string `yaml:"ip"`
}

type QuestDB struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Config struct {
	QuestDB  QuestDB   `yaml:"questdb"`
	Machines []Machine `yaml:"machines"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}
