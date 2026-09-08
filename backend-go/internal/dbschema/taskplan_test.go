package dbschema

import (
	"strings"
	"testing"
)

func TestTaskPlanSchemaIsPresentInBaselineAndUpgrade(t *testing.T) {
	required := []string{
		"CREATE TABLE public.task_plans",
		"CREATE TABLE public.task_plan_steps",
		"plan_sha256 character varying(64)",
		"ck_dsl_generation_runs_plan_binding",
	}
	for _, fragment := range required {
		if !strings.Contains(SchemaSQL, fragment) {
			t.Fatalf("baseline schema missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS public.task_plans",
		"CREATE TABLE IF NOT EXISTS public.task_plan_steps",
		"ADD COLUMN IF NOT EXISTS plan_id",
		"ck_dsl_generation_runs_plan_binding",
	} {
		if !strings.Contains(TaskPlanMigrationSQL, fragment) {
			t.Fatalf("task plan migration missing %q", fragment)
		}
	}
}
