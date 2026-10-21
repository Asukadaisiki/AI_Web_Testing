// Command agenttrace-runtime is the v3 command-consumer process.
package main

import (
	"fmt"
	"log"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/config"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if _, err := config.Load(); err != nil {
		return fmt.Errorf("agenttrace-runtime configuration: %w", err)
	}
	return fmt.Errorf("agenttrace-runtime is not wired yet; complete later v3 tasks before starting it")
}
