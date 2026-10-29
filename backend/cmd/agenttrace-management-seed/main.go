// agenttrace-management-seed bootstraps trusted project bindings and builtin
// immutable resources. It starts no service and never accepts provider secrets.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("agenttrace-management-seed", flag.ContinueOnError)
	lf := flags.String("langfuse-project-id", os.Getenv("AGENTTRACE_LANGFUSE_PROJECT_ID"), "existing authenticated Langfuse project ID")
	project := flags.String("project-id", os.Getenv("AGENTTRACE_PROJECT_ID"), "optional existing Go project ID; empty resolves by LF binding")
	conversation := flags.String("conversation-id", os.Getenv("AGENTTRACE_CONVERSATION_ID"), "optional existing conversation; history is preserved")
	resources := flags.String("resources-dir", os.Getenv("AGENTTRACE_BUILTIN_HARNESS_DIR"), "trusted builtin Harness resource directory")
	actor := flags.String("actor-id", "management-seed", "deployment audit actor")
	timeout := flags.Duration("timeout", 60*time.Second, "bootstrap timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *lf == "" || *resources == "" || *actor == "" || *timeout <= 0 {
		return errors.New("langfuse-project-id, resources-dir and actor-id are required; no positional arguments")
	}
	dsn := os.Getenv("AGENTTRACE_POSTGRES_DSN")
	if dsn == "" {
		return errors.New("AGENTTRACE_POSTGRES_DSN is required")
	}
	builtin, err := execution.LoadBuiltinHarness(*resources, "", "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid PostgreSQL configuration")
	}
	if cfg.MaxConns < 2 {
		cfg.MaxConns = 2
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("PostgreSQL connection failed")
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		return errors.New("PostgreSQL migration failed")
	}
	store := postgres.NewStore(pool)
	store.SetManagementEnabled(true)
	var domains []string
	for _, domain := range strings.Split(os.Getenv("AGENTTRACE_BROWSER_ALLOWED_DOMAINS"), ",") {
		if domain = strings.TrimSpace(domain); domain != "" {
			domains = append(domains, domain)
		}
	}
	if err := store.SetEnvironmentDefaultPolicy(domains); err != nil {
		return errors.New("invalid AGENTTRACE_BROWSER_ALLOWED_DOMAINS deployment policy")
	}
	result, err := store.SeedManagement(ctx, postgres.ManagementSeedParams{
		LangfuseProjectID: *lf, ProjectID: *project, ConversationID: *conversation,
		ActorID: *actor, Builtin: builtin,
	})
	if err != nil {
		// Never include DSNs or arbitrary database diagnostics in console output.
		return errors.New("management bootstrap failed; check IDs, resource revocation and legacy conversation quiescence")
	}
	return json.NewEncoder(stdout).Encode(result)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
