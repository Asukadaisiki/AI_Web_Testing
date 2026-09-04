package config

import (
	"os"
	"strings"
)

type Config struct {
	Address string
}

func Load() Config {
	address := strings.TrimSpace(os.Getenv("AGENTCORE_HTTP_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	return Config{Address: address}
}
