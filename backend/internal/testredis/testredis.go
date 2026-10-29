// Package testredis protects destructive Redis integration test setup.
package testredis

import (
	"fmt"
	"strconv"
	"strings"
)

const composeAddress = "redis:6379"

// Config identifies the only Redis instance and logical database that
// destructive integration tests may reset.
type Config struct {
	Address  string
	Database int
}

// Target permits local Redis and a dedicated logical database.
func Target(address, rawDatabase string) (Config, error) {
	address = strings.TrimSpace(address)
	if address != composeAddress && address != "127.0.0.1:6379" && address != "localhost:6379" {
		return Config{}, fmt.Errorf(
			"refusing destructive Redis reset for address %q: require loopback Redis or Compose service %q",
			address, composeAddress,
		)
	}
	database, err := DatabaseNumber(rawDatabase)
	if err != nil {
		return Config{}, err
	}
	return Config{Address: address, Database: database}, nil
}

// DatabaseNumber accepts only dedicated Redis databases, never the default DB 0.
func DatabaseNumber(raw string) (int, error) {
	database, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || database < 1 || database > 15 {
		return 0, fmt.Errorf(
			"refusing destructive Redis reset for database %q: require a database from 1 through 15",
			raw,
		)
	}
	return database, nil
}
