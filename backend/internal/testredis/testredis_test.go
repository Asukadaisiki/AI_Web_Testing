package testredis

import (
	"strings"
	"testing"
)

func TestDatabaseNumberRejectsDefaultDatabase(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "16", "not-a-number"} {
		if _, err := DatabaseNumber(raw); err == nil {
			t.Fatalf("DatabaseNumber(%q) succeeded, want error", raw)
		}
	}
}

func TestDatabaseNumberAcceptsDedicatedDatabase(t *testing.T) {
	database, err := DatabaseNumber("15")
	if err != nil {
		t.Fatalf("DatabaseNumber: %v", err)
	}
	if database != 15 {
		t.Fatalf("database = %d, want 15", database)
	}
}

func TestTargetRequiresComposeAddressAndDedicatedDatabase(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1:6379", "redis:6380", "live-redis:6379"} {
		if _, err := Target(address, "15"); err == nil {
			t.Fatalf("Target(%q, %q) succeeded, want Compose-address rejection", address, "15")
		}
	}

	target, err := Target("redis:6379", "15")
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	if target.Address != "redis:6379" || target.Database != 15 {
		t.Fatalf("target = %#v, want Compose redis DB 15", target)
	}
}

func TestTargetExplainsTheIsolationContract(t *testing.T) {
	_, err := Target("redis:6379", "0")
	if err == nil {
		t.Fatal("Target accepted live/default Redis")
	}
	if !strings.Contains(err.Error(), "1 through 15") {
		t.Fatalf("Target error = %q, want Redis database isolation contract", err)
	}
	_, err = Target("127.0.0.1:6379", "15")
	if err == nil || !strings.Contains(err.Error(), "redis:6379") {
		t.Fatalf("Target error = %q, want Compose Redis address contract", err)
	}
}
