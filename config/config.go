package config

import (
	"math/rand"
	"notifier/internal/mail_notifier"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Token         string                     `yaml:"token"`
	Mail          mail_notifier.EmailAccount `yaml:"mail"`
	TargetFolder  string                     `yaml:"target_folder"`
	UsersFilepath string                     `yaml:"users_filepath"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	rand.Seed(time.Now().UnixNano())
	return &c, nil
}
