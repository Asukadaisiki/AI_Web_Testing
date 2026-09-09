CREATE TABLE IF NOT EXISTS public.task_plans (
    id character varying(64) PRIMARY KEY,
    schema_version character varying(64) NOT NULL,
    run_id character varying(64) NOT NULL,
    actor_user_id integer,
    project_id integer,
    version integer NOT NULL,
    goal text NOT NULL,
    status character varying(32) NOT NULL,
    plan_sha256 character varying(64) NOT NULL,
    max_side_effect character varying(32) NOT NULL,
    forbidden_actions_json json NOT NULL,
    bound_generation_id integer,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_task_plans_schema_version
        CHECK (schema_version IN ('agent.task_plan.v1', 'agent.task_plan.v2')),
    CONSTRAINT ck_task_plans_version_positive CHECK (version >= 1),
    CONSTRAINT ck_task_plans_hash
        CHECK (length(plan_sha256) = 64 AND lower(plan_sha256) = plan_sha256),
    CONSTRAINT ck_task_plans_status CHECK (
        status IN (
            'grounding', 'ready_for_generation', 'awaiting_approval',
            'approved', 'executing', 'completed', 'failed', 'blocked',
            'superseded'
        )
    ),
    CONSTRAINT ck_task_plans_max_side_effect CHECK (
        max_side_effect IN (
            'none', 'browser_state', 'external_state', 'unknown'
        )
    ),
    CONSTRAINT uq_task_plans_run_version UNIQUE (run_id, version),
    CONSTRAINT fk_task_plans_run
        FOREIGN KEY (run_id) REFERENCES public.agent_runs(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_task_plans_actor
        FOREIGN KEY (actor_user_id) REFERENCES public.users(id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_task_plans_project
        FOREIGN KEY (project_id) REFERENCES public.projects(id)
        ON DELETE SET NULL,
    CONSTRAINT fk_task_plans_generation
        FOREIGN KEY (bound_generation_id)
        REFERENCES public.dsl_generation_runs(id)
        ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS public.task_plan_steps (
    plan_id character varying(64) NOT NULL,
    step_id character varying(64) NOT NULL,
    position integer NOT NULL,
    intent text NOT NULL,
    action character varying(32) NOT NULL,
    target text,
    value text,
    trigger character varying(16),
    context_key character varying(100),
    timeout_ms integer,
    expected_occurrences integer NOT NULL,
    idempotency character varying(32) NOT NULL,
    side_effect character varying(32) NOT NULL,
    preconditions_json json NOT NULL,
    completion_conditions_json json NOT NULL,
    status character varying(32) NOT NULL,
    grounding_attempts integer NOT NULL,
    evidence_refs_json json NOT NULL,
    target_binding_json json,
    PRIMARY KEY (plan_id, step_id),
    CONSTRAINT uq_task_plan_steps_position UNIQUE (plan_id, position),
    CONSTRAINT ck_task_plan_steps_position CHECK (position >= 0),
    CONSTRAINT ck_task_plan_steps_occurrences
        CHECK (expected_occurrences >= 1),
    CONSTRAINT ck_task_plan_steps_grounding_attempts
        CHECK (grounding_attempts >= 0),
    CONSTRAINT ck_task_plan_steps_timeout
        CHECK (timeout_ms IS NULL OR timeout_ms >= 1),
    CONSTRAINT ck_task_plan_steps_action CHECK (
        action IN (
            'goto', 'click', 'input', 'wait_for',
            'assert_text', 'assert_url_contains', 'capture_text'
        )
    ),
    CONSTRAINT ck_task_plan_steps_trigger CHECK (
        trigger IS NULL OR trigger IN ('Enter', 'Tab')
    ),
    CONSTRAINT ck_task_plan_steps_idempotency
        CHECK (idempotency IN ('idempotent', 'non_idempotent')),
    CONSTRAINT ck_task_plan_steps_side_effect CHECK (
        side_effect IN (
            'none', 'browser_state', 'external_state', 'unknown'
        )
    ),
    CONSTRAINT ck_task_plan_steps_status CHECK (
        status IN ('pending', 'grounded', 'failed', 'blocked')
    ),
    CONSTRAINT fk_task_plan_steps_plan
        FOREIGN KEY (plan_id) REFERENCES public.task_plans(id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_task_plans_run_id
    ON public.task_plans (run_id);
CREATE INDEX IF NOT EXISTS ix_task_plans_project_id
    ON public.task_plans (project_id);
CREATE INDEX IF NOT EXISTS ix_task_plans_status
    ON public.task_plans (status);

ALTER TABLE public.task_plan_steps
    ADD COLUMN IF NOT EXISTS target_binding_json json;

ALTER TABLE public.task_plans
    DROP CONSTRAINT IF EXISTS ck_task_plans_schema_version;
ALTER TABLE public.task_plans
    ADD CONSTRAINT ck_task_plans_schema_version
    CHECK (schema_version IN ('agent.task_plan.v1', 'agent.task_plan.v2'));

ALTER TABLE public.dsl_generation_runs
    ADD COLUMN IF NOT EXISTS plan_id character varying(64),
    ADD COLUMN IF NOT EXISTS plan_version integer,
    ADD COLUMN IF NOT EXISTS plan_sha256 character varying(64);

CREATE INDEX IF NOT EXISTS ix_dsl_generation_runs_plan_id
    ON public.dsl_generation_runs (plan_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'ck_dsl_generation_runs_plan_binding'
    ) THEN
        ALTER TABLE public.dsl_generation_runs
            ADD CONSTRAINT ck_dsl_generation_runs_plan_binding CHECK (
                (plan_id IS NULL AND plan_version IS NULL AND plan_sha256 IS NULL)
                OR
                (
                    plan_id IS NOT NULL
                    AND plan_version >= 1
                    AND length(plan_sha256) = 64
                    AND lower(plan_sha256) = plan_sha256
                )
            );
    END IF;
END
$$;
