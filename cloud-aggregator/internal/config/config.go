package config

import (
	"flag"
	"fmt"
	"log"

	"github.com/ilyakaznacheev/cleanenv"
)

type AppConfig struct {
	Server struct {
		Port string `yaml:"port" env:"SERVER_PORT" env-default:"8080"`
		Name string `yaml:"name" env:"SERVER_NAME" env-default:"MPC Node"`
	} `yaml:"server"`

	ZKP struct {
		KeyPath  string `yaml:"key_path" env:"ZKP_KEY_PATH" env-default:"keys/verifying.key"`
		MaxLimit uint64 `yaml:"max_limit" env:"ZKP_MAX_LIMIT" env-default:"10000"`
	} `yaml:"zkp"`

	Aggregator struct {
		ExpectedMeters int `yaml:"expected_meters" env:"EXPECTED_METERS" env-default:"10"`
		// Novi parametri za MP-SPDZ integraciju
		NodeID     int    `yaml:"node_id" env:"NODE_ID" env-default:"0"`
		OutputPath string `yaml:"output_path" env:"OUTPUT_PATH" env-default:"/dev/shm/mp-spdz"`
	} `yaml:"aggregator"`
}

func LoadConfig() (*AppConfig, error) {
	configPath := flag.String("config", "config.yaml", "Path to YAML configuration")
	flag.Parse()

	var cfg AppConfig

	err := cleanenv.ReadConfig(*configPath, &cfg)
	if err != nil {
		log.Println("[INFO] YAML config not found, falling back to Environment variables.")
		if errEnv := cleanenv.ReadEnv(&cfg); errEnv != nil {
			return nil, fmt.Errorf("failed to load environment variables: %w", errEnv)
		}
	}

	// Safety checks
	if cfg.Aggregator.ExpectedMeters < 1 {
		cfg.Aggregator.ExpectedMeters = 10
	}

	return &cfg, nil
}
