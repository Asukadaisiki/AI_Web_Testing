// Command agenttrace-api is the v3 HTTP process.
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
		return fmt.Errorf("agenttrace-api configuration: %w", err)
	}
	return fmt.Errorf("agenttrace-api is not wired yet; complete later v3 tasks before starting it")
}
