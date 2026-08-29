# DOM-Aware DSL Generation Design

## Background

AI-generated DSL targets frequently mismatch actual DOM element attributes. The AI generates semantic descriptions like `"username input"` (element purpose + element type), while the real DOM has `id="username"` or `placeholder="Username"`. The current system has no feedback loop between page DOM and DSL generation — the AI generates targets blindly.

The VLM visual locator (Tier 2) is disabled by default, removing the visual fallback for locator failures.

## Solution

Three independent changes:

1. **`explore_page` tool** — Planning agent tool that visits a URL, collects interactable DOM elements, returns an element inventory to the AI
2. **`capture_page_session` tool** — Planning agent tool that performs login steps and persists browser storage state per project
3. **VLM default enabled** — Change `enable_ai_visual_locate` default from `False` to `True`

## Architecture

```
Planning Agent (ReAct loop)
  │
  ├── explore_page(url)
  │     1. Load project storage_state from file (if exists)
  │     2. Launch temp Playwright context with saved state
  │     3. goto(url), wait for load
  │     4. Run EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT
  │     5. Return element inventory to Agent
  │     6. Close context
  │
  ├── capture_page_session(url, steps)
  │     1. Launch temp Playwright context (no saved state)
  │     2. goto(url), execute login steps sequentially
  │     3. context.storage_state() → save to file
  │     4. Close context
  │
  └── generate_plan (existing)
        Agent includes DOM inventory in draft_prompt
        DSL generator sees real element labels/placeholders/IDs

storage_states/
  {project_id}.json       ← Playwright storage_state format
  {project_id}.meta.json  ← {"source_url", "saved_at"}

VLM Tier 2
  enable_ai_visual_locate: True (default changed)
  Silently skips when VLM_API_KEY not configured
```

## explore_page Tool

### Trigger

Agent calls `explore_page(url="https://example.com/login")` during ReAct loop.

### Flow

1. Read `storage_states/{project_id}.json` — if missing, proceed without state
2. `sync_playwright.chromium.launch()` → `.new_context(storage_state=state)` → `.new_page()`
3. `page.goto(url, wait_until="networkidle")`
4. Run existing `EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT` from `fallback.py` (max 50 elements)
5. Format each element with locator-relevant fields only: `tag`, `id`, `label` (aria-label), `placeholder`, `role`, `text`, `visible`, `enabled`
6. Return formatted text to Agent:
   ```
   input#username [label='Username'] [placeholder='Username']
   input#password [label='Password'] [placeholder='Password']
   button [text='Login']
   ```
7. Close temp context immediately

### Constraints

- Synchronous blocking within ReAct loop (2-5 seconds per call)
- Context closed after collection, no persistent browser resources
- Unreachable URLs return `{"error": "..."}` to Agent, session continues
- Output trimmed to locator-useful fields only, no screenshots

## capture_page_session Tool

### Trigger

Agent calls `capture_page_session(url="...", steps=[...])` when login is needed.

### Flow

1. Launch temp Playwright context (no saved state)
2. `page.goto(url)`
3. Execute steps sequentially using simplified semantic locator (reuse `resolve_semantic_locator`)
4. `context.storage_state()` → write `storage_states/{project_id}.json`
5. Write `storage_states/{project_id}.meta.json` with `{"source_url", "saved_at"}`
6. Close context, return success message with cookie count

### Session State Storage

- Directory: `storage_states/` (configurable via `STORAGE_STATE_DIR`, default `storage_states/`)
- Created on application startup if not exists
- `.meta.json` contains `source_url` and `saved_at` ISO timestamp
- `explore_page` checks `saved_at`: if older than 24 hours, adds `"warning": "session state may be stale"` to response

### Why File Storage

- Playwright `storage_state()` outputs JSON natively — zero conversion
- `new_context(storage_state=path)` natively accepts file path
- Avoids storing large cookie/localStorage blobs in SQLite

## DOM Info → DSL Generation

The ReAct agent receives element inventory as a tool result. When generating a plan, the agent naturally incorporates this information into `draft_prompt` or `generate_plan` action input.

One safeguard in `_build_draft_prompt`:

```
"如果已获取到页面元素清单，请严格按照元素的实际 label、placeholder 或 id 作为 target，不要自行编造描述。"
```

This prompt alone is not guaranteed to work (AI is probabilistic), but combined with actual DOM data in context, target accuracy improves significantly. Remaining mismatches are caught by VLM visual fallback.

## VLM Default Change

```python
# config.py line 69
enable_ai_visual_locate: bool = True  # was False
```

Existing protection in `ai_visual.py` (no changes needed):

```python
if not settings.vlm_api_key or not settings.vlm_model:
    return None  # silently skip when unconfigured
```

## Error Handling

| Scenario | explore_page | capture_page_session |
|---|---|---|
| URL unreachable | `{"error": "页面无法访问: ..."}` | Same |
| storage_state file corrupt | Ignore state, stateless visit | N/A |
| Page load timeout (>10s) | Return partial DOM + timeout warning | Return error |
| No interactable elements | Empty list + warning | Execute steps normally |
| storage_state stale (>24h) | Use state + `warning` in response | N/A |
| Playwright not installed | `{"error": "浏览器引擎未就绪"}` | Same |

## Testing

| Type | Coverage |
|---|---|
| Unit | `explore_page` DOM output formatting, storage_state file I/O, staleness check |
| Unit | `capture_page_session` step execution and state persistence |
| Unit | VLM default enabled + silent skip without key |
| Integration | Full flow: capture → explore → DSL generation → verify target matches real DOM |
| Integration | `explore_page` without storage_state (degraded behavior) |

## Files Changed (Expected)

- `backend/app/ai/planning_tools.py` — add `explore_page` and `capture_page_session` tools
- `backend/app/ai/test_planning_prompts.py` — update system prompt with new tool descriptions
- `backend/app/ai/test_planning_agent.py` — add `_build_draft_prompt` safeguard line
- `backend/app/core/config.py` — `enable_ai_visual_locate` default `True`, add `storage_state_dir`
- `backend/app/main.py` — ensure `storage_states/` directory created on startup
- New: `backend/app/ai/page_explorer.py` — Playwright DOM collection logic (shared by both tools)
- Tests: unit + integration tests for new tools and VLM default
