CREATE TABLE IF NOT EXISTS public.grounding_plans (
    id character varying(64) PRIMARY KEY,
    schema_version character varying(64) NOT NULL,
    run_id character varying(64) NOT NULL,
    task_plan_id character varying(64) NOT NULL,
    task_plan_version integer NOT NULL,
    revision integer NOT NULL,
    status character varying(32) NOT NULL,
    current_plan_step_id character varying(64),
    content_json jsonb NOT NULL,
    content_sha256 character varying(64) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_grounding_plans_schema_version
        CHECK (schema_version = 'grounding.plan.v1'),
    CONSTRAINT ck_grounding_plans_task_plan_version
        CHECK (task_plan_version >= 1),
    CONSTRAINT ck_grounding_plans_revision CHECK (revision >= 1),
    CONSTRAINT ck_grounding_plans_status CHECK (
        status IN ('active', 'ready', 'failed', 'blocked', 'superseded')
    ),
    CONSTRAINT ck_grounding_plans_hash CHECK (
        length(content_sha256) = 64
        AND lower(content_sha256) = content_sha256
    ),
    CONSTRAINT uq_grounding_plans_task_plan_revision
        UNIQUE (task_plan_id, revision),
    CONSTRAINT fk_grounding_plans_run
        FOREIGN KEY (run_id) REFERENCES public.agent_runs(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_grounding_plans_task_plan
        FOREIGN KEY (task_plan_id) REFERENCES public.task_plans(id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_grounding_plans_run_id
    ON public.grounding_plans (run_id);
CREATE INDEX IF NOT EXISTS ix_grounding_plans_task_plan_id
    ON public.grounding_plans (task_plan_id);
CREATE INDEX IF NOT EXISTS ix_grounding_plans_status
    ON public.grounding_plans (status);
