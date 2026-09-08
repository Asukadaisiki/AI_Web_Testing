SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;
SET default_tablespace = '';
SET default_table_access_method = heap;
CREATE TABLE public.agent_events (
    id bigint NOT NULL,
    run_id character varying(64) NOT NULL,
    seq bigint NOT NULL,
    event_type character varying(40) NOT NULL,
    conversation_id character varying(100) NOT NULL,
    step_id character varying(100),
    tool_call_id character varying(100),
    parent_id character varying(100),
    checkpoint_id character varying(100),
    payload_json json NOT NULL,
    created_at timestamp without time zone NOT NULL
);
CREATE SEQUENCE public.agent_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.agent_events_id_seq OWNED BY public.agent_events.id;
CREATE TABLE public.agent_runs (
    id character varying(64) NOT NULL,
    conversation_id character varying(100) NOT NULL,
    project_id integer,
    status character varying(32) NOT NULL,
    input text NOT NULL,
    pending_tool_call_id character varying(100),
    pending_step_id character varying(100),
    transcript_json json NOT NULL,
    last_event_seq bigint DEFAULT '0'::bigint NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    latest_generation_id integer,
    approved_generation_id integer,
    actor_user_id integer,
    CONSTRAINT ck_agent_runs_status CHECK (((status)::text = ANY ((ARRAY['running'::character varying, 'waiting_user'::character varying, 'completed'::character varying, 'failed'::character varying, 'cancelled'::character varying])::text[])))
);
CREATE TABLE public.ai_planning_sessions (
    id integer NOT NULL,
    actor_user_id integer NOT NULL,
    case_id integer,
    title character varying(200),
    status character varying(32) NOT NULL,
    requirements_json json NOT NULL,
    plan_json json,
    missing_slots_json json NOT NULL,
    last_error_message text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    active_project_id integer,
    runtime_owner character varying(16) DEFAULT 'go'::character varying NOT NULL
);
CREATE SEQUENCE public.ai_planning_sessions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.ai_planning_sessions_id_seq OWNED BY public.ai_planning_sessions.id;
CREATE TABLE public.dsl_generation_runs (
    id integer NOT NULL,
    actor_user_id integer NOT NULL,
    prompt_preview character varying(200) NOT NULL,
    prompt_sha256 character varying(64) NOT NULL,
    request_base_url character varying(500),
    generation_mode character varying(32) NOT NULL,
    import_mode character varying(32) NOT NULL,
    model_name character varying(200),
    success boolean NOT NULL,
    error_type character varying(200),
    error_message character varying(2000),
    used_current_case_context boolean NOT NULL,
    used_current_steps_context boolean NOT NULL,
    base_url_source character varying(32) NOT NULL,
    base_url_backfilled boolean NOT NULL,
    repaired_invalid_actions integer NOT NULL,
    removed_invalid_steps integer NOT NULL,
    removed_invalid_contracts integer NOT NULL,
    warnings_count integer NOT NULL,
    normalization_notes_count integer NOT NULL,
    generated_case_json json,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    feedback_status character varying(32) NOT NULL,
    feedback_import_mode character varying(32),
    feedback_recorded_at timestamp without time zone,
    project_id integer,
    case_id integer,
    prompt_version character varying(100) NOT NULL,
    preserve_contracts_requested boolean NOT NULL,
    preserve_contracts_applied boolean NOT NULL,
    warnings_json json NOT NULL,
    normalization_notes_json json NOT NULL,
    rejection_reason_code character varying(64),
    feedback_note character varying(1000),
    prompt_variant character varying(32) NOT NULL,
    context_profile character varying(32) NOT NULL,
    risk_flags_json json NOT NULL,
    retry_from_generation_id integer,
    retry_reason_code character varying(64),
    retry_note character varying(1000),
    governance_focus_reasons_json json NOT NULL,
    dsl_sha256 character varying(64),
    dsl_canonical_version character varying(32),
    plan_id character varying(64),
    plan_version integer,
    plan_sha256 character varying(64),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_base_url_source CHECK (((base_url_source)::text = ANY ((ARRAY['ai_output'::character varying, 'request'::character varying, 'current_case'::character varying, 'none'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_context_profile CHECK (((context_profile)::text = ANY ((ARRAY['blank_request'::character varying, 'rewrite_from_case'::character varying, 'repair_steps'::character varying, 'contracts_focus'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_feedback__53e7 CHECK (((((feedback_status)::text = 'accepted'::text) AND ((feedback_import_mode)::text = ANY ((ARRAY['replace'::character varying, 'steps_only'::character varying, 'contracts_only'::character varying])::text[]))) OR (((feedback_status)::text = ANY ((ARRAY['pending'::character varying, 'rejected'::character varying])::text[])) AND (feedback_import_mode IS NULL)))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_feedback_status CHECK (((feedback_status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying, 'rejected'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_generation_mode CHECK (((generation_mode)::text = ANY ((ARRAY['draft'::character varying, 'strict_steps_only'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_import_mode CHECK (((import_mode)::text = ANY ((ARRAY['replace'::character varying, 'steps_only'::character varying, 'contracts_only'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_prompt_variant CHECK (((prompt_variant)::text = ANY ((ARRAY['baseline_draft'::character varying, 'rewrite_from_case'::character varying, 'repair_steps'::character varying, 'contracts_focus'::character varying])::text[]))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_rejection_3831 CHECK (((((feedback_status)::text = 'rejected'::text) AND ((rejection_reason_code)::text = ANY ((ARRAY['wrong_actions'::character varying, 'invalid_structure'::character varying, 'context_mismatch'::character varying, 'bad_contracts'::character varying, 'other'::character varying])::text[]))) OR (((feedback_status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying])::text[])) AND (rejection_reason_code IS NULL)))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_retry_context CHECK ((((retry_from_generation_id IS NULL) AND (retry_reason_code IS NULL) AND (retry_note IS NULL)) OR ((retry_from_generation_id IS NOT NULL) AND (retry_reason_code IS NOT NULL)))),
    CONSTRAINT ck_dsl_generation_runs_ck_dsl_generation_runs_retry_reason_code CHECK (((retry_reason_code IS NULL) OR ((retry_reason_code)::text = ANY ((ARRAY['wrong_actions'::character varying, 'invalid_structure'::character varying, 'context_mismatch'::character varying, 'bad_contracts'::character varying, 'other'::character varying])::text[])))),
    CONSTRAINT ck_dsl_generation_runs_plan_binding CHECK ((((plan_id IS NULL) AND (plan_version IS NULL) AND (plan_sha256 IS NULL)) OR ((plan_id IS NOT NULL) AND (plan_version >= 1) AND (length((plan_sha256)::text) = 64) AND (lower((plan_sha256)::text) = (plan_sha256)::text))))
);
CREATE SEQUENCE public.dsl_generation_runs_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.dsl_generation_runs_id_seq OWNED BY public.dsl_generation_runs.id;
CREATE TABLE public.task_plans (
    id character varying(64) NOT NULL,
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
    CONSTRAINT ck_task_plans_schema_version CHECK (((schema_version)::text = 'agent.task_plan.v1'::text)),
    CONSTRAINT ck_task_plans_version_positive CHECK ((version >= 1)),
    CONSTRAINT ck_task_plans_hash CHECK (((length((plan_sha256)::text) = 64) AND (lower((plan_sha256)::text) = (plan_sha256)::text))),
    CONSTRAINT ck_task_plans_status CHECK (((status)::text = ANY ((ARRAY['grounding'::character varying, 'ready_for_generation'::character varying, 'awaiting_approval'::character varying, 'approved'::character varying, 'executing'::character varying, 'completed'::character varying, 'failed'::character varying, 'blocked'::character varying, 'superseded'::character varying])::text[]))),
    CONSTRAINT ck_task_plans_max_side_effect CHECK (((max_side_effect)::text = ANY ((ARRAY['none'::character varying, 'browser_state'::character varying, 'external_state'::character varying, 'unknown'::character varying])::text[])))
);
CREATE TABLE public.task_plan_steps (
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
    CONSTRAINT ck_task_plan_steps_position CHECK ((position >= 0)),
    CONSTRAINT ck_task_plan_steps_occurrences CHECK ((expected_occurrences >= 1)),
    CONSTRAINT ck_task_plan_steps_grounding_attempts CHECK ((grounding_attempts >= 0)),
    CONSTRAINT ck_task_plan_steps_timeout CHECK (((timeout_ms IS NULL) OR (timeout_ms >= 1))),
    CONSTRAINT ck_task_plan_steps_action CHECK (((action)::text = ANY ((ARRAY['goto'::character varying, 'click'::character varying, 'input'::character varying, 'wait_for'::character varying, 'assert_text'::character varying, 'assert_url_contains'::character varying, 'capture_text'::character varying])::text[]))),
    CONSTRAINT ck_task_plan_steps_trigger CHECK (((trigger IS NULL) OR ((trigger)::text = ANY ((ARRAY['Enter'::character varying, 'Tab'::character varying])::text[])))),
    CONSTRAINT ck_task_plan_steps_idempotency CHECK (((idempotency)::text = ANY ((ARRAY['idempotent'::character varying, 'non_idempotent'::character varying])::text[]))),
    CONSTRAINT ck_task_plan_steps_side_effect CHECK (((side_effect)::text = ANY ((ARRAY['none'::character varying, 'browser_state'::character varying, 'external_state'::character varying, 'unknown'::character varying])::text[]))),
    CONSTRAINT ck_task_plan_steps_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'grounded'::character varying, 'failed'::character varying, 'blocked'::character varying])::text[])))
);
CREATE TABLE public.execution_batches (
    id integer NOT NULL,
    project_id integer NOT NULL,
    planning_session_id integer,
    triggered_by integer NOT NULL,
    status character varying(32) NOT NULL,
    idempotency_key character varying(100),
    concurrency_limit integer NOT NULL,
    input_values_json json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    started_at timestamp without time zone,
    finished_at timestamp without time zone,
    analysis_status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    analysis_json json,
    CONSTRAINT ck_execution_batches_concurrency_limit_positive CHECK ((concurrency_limit >= 1)),
    CONSTRAINT ck_execution_batches_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'running'::character varying, 'passed'::character varying, 'failed'::character varying, 'needs_intervention'::character varying, 'cancelled'::character varying])::text[])))
);
CREATE SEQUENCE public.execution_batches_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.execution_batches_id_seq OWNED BY public.execution_batches.id;
CREATE TABLE public.execution_jobs (
    id integer NOT NULL,
    batch_id integer NOT NULL,
    project_id integer NOT NULL,
    case_id integer NOT NULL,
    order_index integer NOT NULL,
    status character varying(32) NOT NULL,
    attempt_count integer NOT NULL,
    max_attempts integer NOT NULL,
    lease_owner character varying(200),
    lease_expires_at timestamp without time zone,
    cancel_requested boolean NOT NULL,
    last_error_message text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    started_at timestamp without time zone,
    finished_at timestamp without time zone,
    heartbeat_at timestamp without time zone,
    dsl_snapshot json,
    dsl_canonical_json text,
    dsl_sha256 character varying(64),
    dsl_canonical_version character varying(32),
    CONSTRAINT ck_execution_jobs_attempt_count_non_negative CHECK ((attempt_count >= 0)),
    CONSTRAINT ck_execution_jobs_max_attempts_positive CHECK ((max_attempts >= 1)),
    CONSTRAINT ck_execution_jobs_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'running'::character varying, 'passed'::character varying, 'failed'::character varying, 'needs_intervention'::character varying, 'cancelled'::character varying])::text[])))
);
CREATE SEQUENCE public.execution_jobs_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.execution_jobs_id_seq OWNED BY public.execution_jobs.id;
CREATE TABLE public.locator_correction_events (
    id integer NOT NULL,
    correction_id integer NOT NULL,
    event_type character varying(32) NOT NULL,
    page_url_pattern character varying(500) NOT NULL,
    target_description character varying(200) NOT NULL,
    execution_id integer,
    verified_count_after integer NOT NULL,
    consecutive_failures_after integer NOT NULL,
    is_active_after boolean NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_locator_correction_events_ck_locator_correction_even_05d1 CHECK (((event_type)::text = ANY ((ARRAY['created'::character varying, 'activated'::character varying, 'deactivated'::character varying, 'tier0_hit'::character varying, 'tier0_miss'::character varying, 'auto_deactivated'::character varying])::text[])))
);
CREATE SEQUENCE public.locator_correction_events_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.locator_correction_events_id_seq OWNED BY public.locator_correction_events.id;
CREATE TABLE public.locator_corrections (
    id integer NOT NULL,
    page_url_pattern character varying(500) NOT NULL,
    target_description character varying(200) NOT NULL,
    normalized_target_description character varying(200) NOT NULL,
    correction_type character varying(20) NOT NULL,
    correction_value text NOT NULL,
    verified_count integer NOT NULL,
    consecutive_failures integer NOT NULL,
    is_active boolean NOT NULL,
    source_execution_id integer,
    created_by integer NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    CONSTRAINT ck_locator_corrections_v2_ck_locator_corrections_correc_aa9b CHECK (((correction_type)::text = ANY ((ARRAY['css'::character varying, 'xpath'::character varying, 'test_id'::character varying])::text[])))
);
CREATE SEQUENCE public.locator_corrections_v2_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.locator_corrections_v2_id_seq OWNED BY public.locator_corrections.id;
CREATE TABLE public.project_members (
    id integer NOT NULL,
    project_id integer NOT NULL,
    user_id integer NOT NULL,
    role character varying(50) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);
CREATE SEQUENCE public.project_members_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.project_members_id_seq OWNED BY public.project_members.id;
CREATE TABLE public.projects (
    id integer NOT NULL,
    name character varying(200) NOT NULL,
    description character varying(1000),
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    is_default boolean DEFAULT false NOT NULL
);
CREATE SEQUENCE public.projects_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.projects_id_seq OWNED BY public.projects.id;
CREATE TABLE public.research_experiments (
    id character varying(64) NOT NULL,
    project_id integer NOT NULL,
    name character varying(200) NOT NULL,
    goal text NOT NULL,
    dataset_version character varying(200) NOT NULL,
    model_provider character varying(200) NOT NULL,
    model_name character varying(200) NOT NULL,
    model_version character varying(200) NOT NULL,
    prompt_version character varying(200) NOT NULL,
    browser_name character varying(200) NOT NULL,
    browser_version character varying(200) NOT NULL,
    viewport_json json NOT NULL,
    code_sha256 character varying(64) NOT NULL,
    policy_version character varying(200) NOT NULL,
    observation_profile character varying(200) NOT NULL,
    dsl_profile character varying(200) NOT NULL,
    seed bigint NOT NULL,
    variant character varying(200) NOT NULL,
    repetitions integer NOT NULL,
    status character varying(32) NOT NULL,
    config_json json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_research_experiments_code_sha256 CHECK (((length((code_sha256)::text) = 64) AND (lower((code_sha256)::text) = (code_sha256)::text))),
    CONSTRAINT ck_research_experiments_policy_version CHECK (((policy_version)::text = 'research.policy.v1'::text)),
    CONSTRAINT ck_research_experiments_repetitions_positive CHECK ((repetitions >= 1)),
    CONSTRAINT ck_research_experiments_status CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'active'::character varying, 'completed'::character varying, 'cancelled'::character varying])::text[])))
);
CREATE TABLE public.research_oracle_results (
    research_run_id character varying(64) NOT NULL,
    execution_id integer NOT NULL,
    schema_version character varying(64) NOT NULL,
    evaluator character varying(200) NOT NULL,
    passed boolean NOT NULL,
    reason_code character varying(100) NOT NULL,
    decision_json json NOT NULL,
    content_sha256 character varying(64) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_research_oracle_results_content_sha256 CHECK (((length((content_sha256)::text) = 64) AND (lower((content_sha256)::text) = (content_sha256)::text))),
    CONSTRAINT ck_research_oracle_results_schema_version CHECK (((schema_version)::text = 'research.oracle.v1'::text))
);
CREATE TABLE public.research_runs (
    id character varying(64) NOT NULL,
    experiment_id character varying(64) NOT NULL,
    project_id integer NOT NULL,
    idempotency_key character varying(200) NOT NULL,
    repetition_index integer NOT NULL,
    warmup boolean NOT NULL,
    status character varying(32) NOT NULL,
    schema_version character varying(64) NOT NULL,
    projector_version character varying(64) NOT NULL,
    metric_version character varying(64) NOT NULL,
    policy_version character varying(64) NOT NULL,
    agent_run_id character varying(64),
    generation_id integer,
    batch_id integer,
    execution_id integer,
    dsl_sha256 character varying(64),
    metrics_json json,
    started_at timestamp without time zone,
    finished_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_research_runs_dsl_sha256 CHECK (((dsl_sha256 IS NULL) OR ((length((dsl_sha256)::text) = 64) AND (lower((dsl_sha256)::text) = (dsl_sha256)::text)))),
    CONSTRAINT ck_research_runs_link_prefix CHECK ((((generation_id IS NULL) OR (agent_run_id IS NOT NULL)) AND ((batch_id IS NULL) OR (generation_id IS NOT NULL)) AND ((execution_id IS NULL) OR (batch_id IS NOT NULL)) AND ((dsl_sha256 IS NULL) OR (generation_id IS NOT NULL)))),
    CONSTRAINT ck_research_runs_repetition_index_non_negative CHECK ((repetition_index >= 0)),
    CONSTRAINT ck_research_runs_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'running'::character varying, 'completed'::character varying, 'failed'::character varying, 'cancelled'::character varying])::text[]))),
    CONSTRAINT ck_research_runs_status_timestamps CHECK (((((status)::text = 'pending'::text) AND (started_at IS NULL) AND (finished_at IS NULL)) OR (((status)::text = 'running'::text) AND (started_at IS NOT NULL) AND (finished_at IS NULL)) OR (((status)::text = ANY ((ARRAY['completed'::character varying, 'failed'::character varying])::text[])) AND (started_at IS NOT NULL) AND (finished_at IS NOT NULL)) OR (((status)::text = 'cancelled'::text) AND (finished_at IS NOT NULL)))),
    CONSTRAINT ck_research_runs_timestamp_order CHECK (((started_at IS NULL) OR (finished_at IS NULL) OR (finished_at >= started_at))),
    CONSTRAINT ck_research_runs_versions CHECK ((((schema_version)::text = 'research.persistence.v1'::text) AND ((projector_version)::text = 'research.projector.v1'::text) AND ((metric_version)::text = 'research.metrics.v1'::text) AND ((policy_version)::text = 'research.policy.v1'::text)))
);
CREATE TABLE public.research_transitions (
    id bigint NOT NULL,
    research_run_id character varying(64) NOT NULL,
    ordinal bigint NOT NULL,
    append_key character varying(200) NOT NULL,
    content_sha256 character varying(64) NOT NULL,
    schema_version character varying(64) NOT NULL,
    transition_json json NOT NULL,
    artifact_refs_json json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_research_transitions_content_sha256 CHECK (((length((content_sha256)::text) = 64) AND (lower((content_sha256)::text) = (content_sha256)::text))),
    CONSTRAINT ck_research_transitions_ordinal_non_negative CHECK ((ordinal >= 0)),
    CONSTRAINT ck_research_transitions_schema_version CHECK (((schema_version)::text = 'research.persistence.v1'::text))
);
CREATE SEQUENCE public.research_transitions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.research_transitions_id_seq OWNED BY public.research_transitions.id;
CREATE TABLE public.session_projects (
    id integer NOT NULL,
    session_id integer NOT NULL,
    project_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);
CREATE SEQUENCE public.session_projects_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.session_projects_id_seq OWNED BY public.session_projects.id;
CREATE TABLE public.test_case_runs (
    id integer NOT NULL,
    case_id integer NOT NULL,
    project_id integer NOT NULL,
    triggered_by integer NOT NULL,
    status character varying(20) NOT NULL,
    error_message text,
    report json,
    started_at timestamp without time zone DEFAULT now() NOT NULL,
    finished_at timestamp without time zone,
    batch_id integer,
    job_id integer,
    attempt_number integer NOT NULL,
    dsl_snapshot json,
    dsl_sha256 character varying(64),
    report_schema_version character varying(32) NOT NULL,
    failure_signal_json json,
    analysis_status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    analysis_json json
);
CREATE SEQUENCE public.test_case_runs_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.test_case_runs_id_seq OWNED BY public.test_case_runs.id;
CREATE TABLE public.test_cases (
    id integer NOT NULL,
    project_id integer NOT NULL,
    created_by integer NOT NULL,
    updated_by integer NOT NULL,
    name character varying(200) NOT NULL,
    description character varying(1000),
    dsl json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);
CREATE SEQUENCE public.test_cases_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.test_cases_id_seq OWNED BY public.test_cases.id;
CREATE TABLE public.users (
    id integer NOT NULL,
    email character varying(255) NOT NULL,
    display_name character varying(100) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);
CREATE SEQUENCE public.users_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;
ALTER TABLE ONLY public.agent_events ALTER COLUMN id SET DEFAULT nextval('public.agent_events_id_seq'::regclass);
ALTER TABLE ONLY public.ai_planning_sessions ALTER COLUMN id SET DEFAULT nextval('public.ai_planning_sessions_id_seq'::regclass);
ALTER TABLE ONLY public.dsl_generation_runs ALTER COLUMN id SET DEFAULT nextval('public.dsl_generation_runs_id_seq'::regclass);
ALTER TABLE ONLY public.execution_batches ALTER COLUMN id SET DEFAULT nextval('public.execution_batches_id_seq'::regclass);
ALTER TABLE ONLY public.execution_jobs ALTER COLUMN id SET DEFAULT nextval('public.execution_jobs_id_seq'::regclass);
ALTER TABLE ONLY public.locator_correction_events ALTER COLUMN id SET DEFAULT nextval('public.locator_correction_events_id_seq'::regclass);
ALTER TABLE ONLY public.locator_corrections ALTER COLUMN id SET DEFAULT nextval('public.locator_corrections_v2_id_seq'::regclass);
ALTER TABLE ONLY public.project_members ALTER COLUMN id SET DEFAULT nextval('public.project_members_id_seq'::regclass);
ALTER TABLE ONLY public.projects ALTER COLUMN id SET DEFAULT nextval('public.projects_id_seq'::regclass);
ALTER TABLE ONLY public.research_transitions ALTER COLUMN id SET DEFAULT nextval('public.research_transitions_id_seq'::regclass);
ALTER TABLE ONLY public.session_projects ALTER COLUMN id SET DEFAULT nextval('public.session_projects_id_seq'::regclass);
ALTER TABLE ONLY public.test_case_runs ALTER COLUMN id SET DEFAULT nextval('public.test_case_runs_id_seq'::regclass);
ALTER TABLE ONLY public.test_cases ALTER COLUMN id SET DEFAULT nextval('public.test_cases_id_seq'::regclass);
ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);
ALTER TABLE ONLY public.agent_events
    ADD CONSTRAINT pk_agent_events PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT pk_agent_runs PRIMARY KEY (id);
ALTER TABLE ONLY public.ai_planning_sessions
    ADD CONSTRAINT pk_ai_planning_sessions PRIMARY KEY (id);
ALTER TABLE ONLY public.dsl_generation_runs
    ADD CONSTRAINT pk_dsl_generation_runs PRIMARY KEY (id);
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT pk_task_plans PRIMARY KEY (id);
ALTER TABLE ONLY public.task_plan_steps
    ADD CONSTRAINT pk_task_plan_steps PRIMARY KEY (plan_id, step_id);
ALTER TABLE ONLY public.execution_batches
    ADD CONSTRAINT pk_execution_batches PRIMARY KEY (id);
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT pk_execution_jobs PRIMARY KEY (id);
ALTER TABLE ONLY public.locator_correction_events
    ADD CONSTRAINT pk_locator_correction_events PRIMARY KEY (id);
ALTER TABLE ONLY public.locator_corrections
    ADD CONSTRAINT pk_locator_corrections_v2 PRIMARY KEY (id);
ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT pk_project_members PRIMARY KEY (id);
ALTER TABLE ONLY public.projects
    ADD CONSTRAINT pk_projects PRIMARY KEY (id);
ALTER TABLE ONLY public.research_experiments
    ADD CONSTRAINT pk_research_experiments PRIMARY KEY (id);
ALTER TABLE ONLY public.research_oracle_results
    ADD CONSTRAINT pk_research_oracle_results PRIMARY KEY (research_run_id);
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT pk_research_runs PRIMARY KEY (id);
ALTER TABLE ONLY public.research_transitions
    ADD CONSTRAINT pk_research_transitions PRIMARY KEY (id);
ALTER TABLE ONLY public.session_projects
    ADD CONSTRAINT pk_session_projects PRIMARY KEY (id);
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT pk_test_case_runs PRIMARY KEY (id);
ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT pk_test_cases PRIMARY KEY (id);
ALTER TABLE ONLY public.users
    ADD CONSTRAINT pk_users PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_events
    ADD CONSTRAINT uq_agent_events_run_seq UNIQUE (run_id, seq);
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT uq_task_plans_run_version UNIQUE (run_id, version);
ALTER TABLE ONLY public.task_plan_steps
    ADD CONSTRAINT uq_task_plan_steps_position UNIQUE (plan_id, position);
ALTER TABLE ONLY public.execution_batches
    ADD CONSTRAINT uq_execution_batches_actor_idempotency UNIQUE (triggered_by, idempotency_key);
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT uq_execution_jobs_batch_case UNIQUE (batch_id, case_id);
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT uq_execution_jobs_batch_order UNIQUE (batch_id, order_index);
ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT uq_project_members_project_id UNIQUE (project_id, user_id);
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT uq_research_runs_experiment_idempotency UNIQUE (experiment_id, idempotency_key);
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT uq_research_runs_experiment_repetition_warmup UNIQUE (experiment_id, repetition_index, warmup);
ALTER TABLE ONLY public.research_transitions
    ADD CONSTRAINT uq_research_transitions_run_append_key UNIQUE (research_run_id, append_key);
ALTER TABLE ONLY public.research_transitions
    ADD CONSTRAINT uq_research_transitions_run_ordinal UNIQUE (research_run_id, ordinal);
ALTER TABLE ONLY public.session_projects
    ADD CONSTRAINT uq_session_projects UNIQUE (session_id, project_id);
CREATE INDEX ix_agent_events_run_created ON public.agent_events USING btree (run_id, created_at);
CREATE INDEX ix_agent_events_run_id ON public.agent_events USING btree (run_id);
CREATE INDEX ix_agent_runs_actor_user_id ON public.agent_runs USING btree (actor_user_id);
CREATE INDEX ix_agent_runs_conversation_id ON public.agent_runs USING btree (conversation_id);
CREATE INDEX ix_agent_runs_project_id ON public.agent_runs USING btree (project_id);
CREATE INDEX ix_agent_runs_status ON public.agent_runs USING btree (status);
CREATE INDEX ix_ai_planning_sessions_active_project_id ON public.ai_planning_sessions USING btree (active_project_id);
CREATE INDEX ix_ai_planning_sessions_actor_user_id ON public.ai_planning_sessions USING btree (actor_user_id);
CREATE INDEX ix_ai_planning_sessions_case_id ON public.ai_planning_sessions USING btree (case_id);
CREATE INDEX ix_ai_planning_sessions_created_at ON public.ai_planning_sessions USING btree (created_at);
CREATE INDEX ix_ai_planning_sessions_runtime_owner ON public.ai_planning_sessions USING btree (runtime_owner);
CREATE INDEX ix_dsl_generation_runs_actor_user_id ON public.dsl_generation_runs USING btree (actor_user_id);
CREATE INDEX ix_dsl_generation_runs_case_id ON public.dsl_generation_runs USING btree (case_id);
CREATE INDEX ix_dsl_generation_runs_created_at ON public.dsl_generation_runs USING btree (created_at);
CREATE INDEX ix_dsl_generation_runs_dsl_sha256 ON public.dsl_generation_runs USING btree (dsl_sha256);
CREATE INDEX ix_dsl_generation_runs_project_id ON public.dsl_generation_runs USING btree (project_id);
CREATE INDEX ix_dsl_generation_runs_plan_id ON public.dsl_generation_runs USING btree (plan_id);
CREATE INDEX ix_dsl_generation_runs_prompt_sha256 ON public.dsl_generation_runs USING btree (prompt_sha256);
CREATE INDEX ix_dsl_generation_runs_retry_from_generation_id ON public.dsl_generation_runs USING btree (retry_from_generation_id);
CREATE INDEX ix_dsl_generation_runs_success ON public.dsl_generation_runs USING btree (success);
CREATE INDEX ix_execution_batches_created_at ON public.execution_batches USING btree (created_at);
CREATE INDEX ix_execution_batches_planning_session_id ON public.execution_batches USING btree (planning_session_id);
CREATE INDEX ix_execution_batches_project_id ON public.execution_batches USING btree (project_id);
CREATE INDEX ix_execution_batches_status ON public.execution_batches USING btree (status);
CREATE INDEX ix_execution_batches_triggered_by ON public.execution_batches USING btree (triggered_by);
CREATE INDEX ix_execution_jobs_batch_id ON public.execution_jobs USING btree (batch_id);
CREATE INDEX ix_execution_jobs_case_id ON public.execution_jobs USING btree (case_id);
CREATE INDEX ix_execution_jobs_created_at ON public.execution_jobs USING btree (created_at);
CREATE INDEX ix_execution_jobs_lease_expires_at ON public.execution_jobs USING btree (lease_expires_at);
CREATE INDEX ix_execution_jobs_lease_owner ON public.execution_jobs USING btree (lease_owner);
CREATE INDEX ix_execution_jobs_project_id ON public.execution_jobs USING btree (project_id);
CREATE INDEX ix_execution_jobs_status ON public.execution_jobs USING btree (status);
CREATE INDEX ix_locator_correction_events_correction_id ON public.locator_correction_events USING btree (correction_id);
CREATE INDEX ix_locator_correction_events_event_type ON public.locator_correction_events USING btree (event_type);
CREATE INDEX ix_locator_correction_events_execution_id ON public.locator_correction_events USING btree (execution_id);
CREATE INDEX ix_locator_corrections_created_by ON public.locator_corrections USING btree (created_by);
CREATE INDEX ix_locator_corrections_lookup ON public.locator_corrections USING btree (page_url_pattern, normalized_target_description);
CREATE INDEX ix_locator_corrections_source_execution_id ON public.locator_corrections USING btree (source_execution_id);
CREATE INDEX ix_project_members_project_id ON public.project_members USING btree (project_id);
CREATE INDEX ix_project_members_user_id ON public.project_members USING btree (user_id);
CREATE UNIQUE INDEX ix_projects_name ON public.projects USING btree (name);
CREATE INDEX ix_research_experiments_project_created ON public.research_experiments USING btree (project_id, created_at);
CREATE INDEX ix_research_experiments_project_id ON public.research_experiments USING btree (project_id);
CREATE INDEX ix_research_experiments_status ON public.research_experiments USING btree (status);
CREATE INDEX ix_research_oracle_results_execution_id ON public.research_oracle_results USING btree (execution_id);
CREATE INDEX ix_research_runs_agent_run_id ON public.research_runs USING btree (agent_run_id);
CREATE INDEX ix_research_runs_batch_id ON public.research_runs USING btree (batch_id);
CREATE INDEX ix_research_runs_execution_id ON public.research_runs USING btree (execution_id);
CREATE INDEX ix_research_runs_experiment_created ON public.research_runs USING btree (experiment_id, created_at);
CREATE INDEX ix_research_runs_experiment_id ON public.research_runs USING btree (experiment_id);
CREATE INDEX ix_research_runs_generation_id ON public.research_runs USING btree (generation_id);
CREATE INDEX ix_research_runs_project_id ON public.research_runs USING btree (project_id);
CREATE INDEX ix_research_runs_status ON public.research_runs USING btree (status);
CREATE INDEX ix_research_transitions_research_run_id ON public.research_transitions USING btree (research_run_id);
CREATE INDEX ix_research_transitions_run_created ON public.research_transitions USING btree (research_run_id, created_at);
CREATE INDEX ix_session_projects_project_id ON public.session_projects USING btree (project_id);
CREATE INDEX ix_session_projects_session_id ON public.session_projects USING btree (session_id);
CREATE INDEX ix_test_case_runs_batch_id ON public.test_case_runs USING btree (batch_id);
CREATE INDEX ix_test_case_runs_case_id ON public.test_case_runs USING btree (case_id);
CREATE INDEX ix_test_case_runs_dsl_sha256 ON public.test_case_runs USING btree (dsl_sha256);
CREATE INDEX ix_test_case_runs_job_id ON public.test_case_runs USING btree (job_id);
CREATE INDEX ix_test_case_runs_project_id ON public.test_case_runs USING btree (project_id);
CREATE INDEX ix_test_case_runs_status ON public.test_case_runs USING btree (status);
CREATE INDEX ix_test_case_runs_triggered_by ON public.test_case_runs USING btree (triggered_by);
CREATE INDEX ix_test_cases_created_by ON public.test_cases USING btree (created_by);
CREATE INDEX ix_test_cases_name ON public.test_cases USING btree (name);
CREATE INDEX ix_test_cases_project_id ON public.test_cases USING btree (project_id);
CREATE INDEX ix_test_cases_updated_by ON public.test_cases USING btree (updated_by);
CREATE INDEX ix_task_plans_project_id ON public.task_plans USING btree (project_id);
CREATE INDEX ix_task_plans_run_id ON public.task_plans USING btree (run_id);
CREATE INDEX ix_task_plans_status ON public.task_plans USING btree (status);
CREATE UNIQUE INDEX ix_users_email ON public.users USING btree (email);
CREATE UNIQUE INDEX uq_locator_corrections_active_lookup ON public.locator_corrections USING btree (page_url_pattern, normalized_target_description) WHERE is_active;
ALTER TABLE ONLY public.agent_events
    ADD CONSTRAINT fk_agent_events_run_id_agent_runs FOREIGN KEY (run_id) REFERENCES public.agent_runs(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT fk_agent_runs_actor_user_id_users FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT fk_agent_runs_approved_generation_id FOREIGN KEY (approved_generation_id) REFERENCES public.dsl_generation_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT fk_agent_runs_latest_generation_id FOREIGN KEY (latest_generation_id) REFERENCES public.dsl_generation_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent_runs
    ADD CONSTRAINT fk_agent_runs_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.ai_planning_sessions
    ADD CONSTRAINT fk_ai_planning_sessions_active_project_id_projects FOREIGN KEY (active_project_id) REFERENCES public.projects(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.ai_planning_sessions
    ADD CONSTRAINT fk_ai_planning_sessions_actor_user_id_users FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.ai_planning_sessions
    ADD CONSTRAINT fk_ai_planning_sessions_case_id_test_cases FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.dsl_generation_runs
    ADD CONSTRAINT fk_dsl_generation_runs_actor_user_id_users FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.dsl_generation_runs
    ADD CONSTRAINT fk_dsl_generation_runs_case_id_test_cases FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.dsl_generation_runs
    ADD CONSTRAINT fk_dsl_generation_runs_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.dsl_generation_runs
    ADD CONSTRAINT fk_dsl_generation_runs_retry_from_generation_id FOREIGN KEY (retry_from_generation_id) REFERENCES public.dsl_generation_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT fk_task_plans_run FOREIGN KEY (run_id) REFERENCES public.agent_runs(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT fk_task_plans_actor FOREIGN KEY (actor_user_id) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT fk_task_plans_project FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.task_plans
    ADD CONSTRAINT fk_task_plans_generation FOREIGN KEY (bound_generation_id) REFERENCES public.dsl_generation_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.task_plan_steps
    ADD CONSTRAINT fk_task_plan_steps_plan FOREIGN KEY (plan_id) REFERENCES public.task_plans(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.execution_batches
    ADD CONSTRAINT fk_execution_batches_planning_session_id_ai_planning_sessions FOREIGN KEY (planning_session_id) REFERENCES public.ai_planning_sessions(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.execution_batches
    ADD CONSTRAINT fk_execution_batches_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.execution_batches
    ADD CONSTRAINT fk_execution_batches_triggered_by_users FOREIGN KEY (triggered_by) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT fk_execution_jobs_batch_id_execution_batches FOREIGN KEY (batch_id) REFERENCES public.execution_batches(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT fk_execution_jobs_case_id_test_cases FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.execution_jobs
    ADD CONSTRAINT fk_execution_jobs_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.locator_correction_events
    ADD CONSTRAINT fk_locator_correction_events_correction_id_locator_corrections FOREIGN KEY (correction_id) REFERENCES public.locator_corrections(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.locator_correction_events
    ADD CONSTRAINT fk_locator_correction_events_execution_id_test_case_runs FOREIGN KEY (execution_id) REFERENCES public.test_case_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.locator_corrections
    ADD CONSTRAINT fk_locator_corrections_v2_created_by_users FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.locator_corrections
    ADD CONSTRAINT fk_locator_corrections_v2_source_execution_id_test_case_runs FOREIGN KEY (source_execution_id) REFERENCES public.test_case_runs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT fk_project_members_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT fk_project_members_user_id_users FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.research_experiments
    ADD CONSTRAINT fk_research_experiments_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_oracle_results
    ADD CONSTRAINT fk_research_oracle_results_execution_id_test_case_runs FOREIGN KEY (execution_id) REFERENCES public.test_case_runs(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_oracle_results
    ADD CONSTRAINT fk_research_oracle_results_research_run_id_research_runs FOREIGN KEY (research_run_id) REFERENCES public.research_runs(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_agent_run_id_agent_runs FOREIGN KEY (agent_run_id) REFERENCES public.agent_runs(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_batch_id_execution_batches FOREIGN KEY (batch_id) REFERENCES public.execution_batches(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_execution_id_test_case_runs FOREIGN KEY (execution_id) REFERENCES public.test_case_runs(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_experiment_id_research_experiments FOREIGN KEY (experiment_id) REFERENCES public.research_experiments(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_generation_id_dsl_generation_runs FOREIGN KEY (generation_id) REFERENCES public.dsl_generation_runs(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_runs
    ADD CONSTRAINT fk_research_runs_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.research_transitions
    ADD CONSTRAINT fk_research_transitions_research_run_id_research_runs FOREIGN KEY (research_run_id) REFERENCES public.research_runs(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.session_projects
    ADD CONSTRAINT fk_session_projects_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.session_projects
    ADD CONSTRAINT fk_session_projects_session_id_ai_planning_sessions FOREIGN KEY (session_id) REFERENCES public.ai_planning_sessions(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT fk_test_case_runs_batch_id_execution_batches FOREIGN KEY (batch_id) REFERENCES public.execution_batches(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT fk_test_case_runs_case_id_test_cases FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT fk_test_case_runs_job_id_execution_jobs FOREIGN KEY (job_id) REFERENCES public.execution_jobs(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT fk_test_case_runs_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.test_case_runs
    ADD CONSTRAINT fk_test_case_runs_triggered_by_users FOREIGN KEY (triggered_by) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT fk_test_cases_created_by_users FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT fk_test_cases_project_id_projects FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT fk_test_cases_updated_by_users FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE RESTRICT;
