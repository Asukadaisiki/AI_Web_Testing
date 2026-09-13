package dbschema

import (
	"strings"
	"testing"
)

func TestTaskPlanSchemaIsPresentInBaselineAndUpgrade(t *testing.T) {
	required := []string{
		"CREATE TABLE public.task_plans",
		"CREATE TABLE public.task_plan_steps",
		"target_binding_json json",
		"'agent.task_plan.v2'",
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
		"ADD COLUMN IF NOT EXISTS target_binding_json",
		"'agent.task_plan.v2'",
		"ADD COLUMN IF NOT EXISTS plan_id",
		"ck_dsl_generation_runs_plan_binding",
	} {
		if !strings.Contains(TaskPlanMigrationSQL, fragment) {
			t.Fatalf("task plan migration missing %q", fragment)
		}
	}
}

func TestGroundingPlanSchemaIsPresentInBaselineAndUpgrade(t *testing.T) {
	required := []string{
		"CREATE TABLE public.grounding_plans",
		"content_json jsonb",
		"content_sha256 character varying(64)",
		"uq_grounding_plans_task_plan_revision",
		"fk_grounding_plans_task_plan",
	}
	for _, fragment := range required {
		if !strings.Contains(SchemaSQL, fragment) {
			t.Fatalf("baseline schema missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS public.grounding_plans",
		"content_json jsonb",
		"content_sha256 character varying(64)",
		"uq_grounding_plans_task_plan_revision",
		"fk_grounding_plans_task_plan",
	} {
		if !strings.Contains(GroundingPlanMigrationSQL, fragment) {
			t.Fatalf("grounding plan migration missing %q", fragment)
		}
	}
}
