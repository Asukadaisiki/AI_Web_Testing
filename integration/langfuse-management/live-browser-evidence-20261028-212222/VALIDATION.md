# Real Langfuse UI Acceptance

Executed 2026-10-28, 13:22:22-13:38:46 UTC, against Langfuse production build `.next-management-20261028-03` on port 3300. The parent restarted the managed API/worker/runtime during this session; UI mutations were held until management health on port 8180 returned 200.

## Assessment

Core supported-input UI workflows verified. All UI submissions and mutations are DONE. The only owned UI run is terminal/cancelled, takeover is stopped, the worker drain flag is restored, and the empty conversation is deleted. The retained owned environment is ready at profile generation 2.

`report.json` preserves **61 passing checks, 8 failed exploratory checks, and 33 screenshots**. This is not an all-green test-suite claim. Original false checks are unchanged; explicit corrections are appended in `corrections` and `owned.assessment_corrections`.

## Actual UI Evidence

| Workflow | Observed Result | Screenshot |
| --- | --- | --- |
| Login and animated modal | Private credentials entered in actual login form. Isolated Chromium dialog reached opacity 1, animation currentTime 800 and finished state. | 03 |
| Two owned conversations | Distinct environments automatically became ready. Seed and parent-owned conversations were not edited or deleted. | 02, 04 |
| Skill folder upload | Real local root SKILL.md uploaded through the file input. Valid slug produced draft content/digest preview, then published status. | 06, 07 |
| Existing MCP binding | Approved browser-harness version loaded for inspection, selected in Harness composition, and used by the real run. Binding configuration was not rewritten. | 08, 09, 12, 14 |
| Harness publication/edit | Owned immutable revisions 1, 2 and 3 published. Scoped textbox assertion verified the original prompt survived editing; revision 3 changed it. | 09, 22, 23, 24 |
| Real Pi run/cancel | Actual open_page and assert_text operations appeared before cancellation; durable status became cancelled and native Trace exported. | 12, 13, 14 |
| Historical snapshot | After API restart and newer Harness revisions, the run still displayed its original version ID and content digest. | 16, 25 |
| Takeover | Actual controlled frame navigated to Demo Catalog. Frame click, Type text "Alpha", and Send key Enter visibly filtered the results to Alpha only. Stop removed the frame and returned the environment to ready. | 17, 18, 19 |
| Profile reset | Real confirmation submitted; generation increased from 1 to 2 and environment returned to ready. | 20, 21, 32 |
| Services drain/restore | Offline, project-bound runtime registration with zero active work switched Drain to Resume and back. Active parent workers were untouched. | 26, 27, 28 |
| Archive/delete | Empty owned conversation archived; New agent run disabled. Explicit delete confirmation submitted; deleting option then disappeared from the selector. | 29, 30, 31, 32 |
| Canonical image rendering | Authenticated Screenshots section rendered the actual run-owned Demo Catalog PNG. | 33 |

## Corrections

The invalid spaced Skill name was correctly rejected by backend slug validation. This is not a failure of valid-package upload. The remaining **frontend validation/feedback mismatch** is that the form permits spaces and up to 200 characters, then displays generic BAD_REQUEST instead of guiding the user to a valid slug. Screenshot 05 records that rejection; screenshots 06-07 verify recovery with a valid name.

The two initial draft/Preview failures refer to the rejected invalid name. The first `skill-draft-preview` check only located SKILL.md in upload copy and is insufficient alone; screenshot 06 is the actual preview evidence.

The remaining six false checks were resolved test locators: the MCP select label included option text; transformed capitalization prevented the open_page Output text match; recovery buttons had already disappeared automatically; the Run ID is a table cell rather than a button; the prefilled prompt needed a scoped textbox locator. The corrected actions/assertions are preserved separately. No false result was changed to true.

An earlier startup attempt remains in `../live-browser-evidence-20261028-212112/report.json`: it stopped before login because the actual label was "Password (forgot password?)", not "Password". It made no acceptance writes and is not included in the 69 checks of the completed session.

The brief native Trace not-found response resolved before screenshot 14. Parent-controlled API downtime and the environment-deletion race generated toasts that remained visible after recovery. These stale toasts were not hidden or converted into successful checks; final selected run/environment state was verified independently.

## Fix Handoff

Parent application owner was given these exact paths; this evidence task did not change application code or restart Langfuse:

- `/Users/bytedance/project/langfuse-agenttrace/web/src/features/browser-runs/components/RegistryPanel.tsx:329-332`, SkillUpload Skill-name input.
- `/Users/bytedance/project/langfuse-agenttrace/web/src/features/browser-runs/management-contracts.ts:82`, SkillUploadSchema.
- Backend authority: `/Users/bytedance/project/AI_Web_Testing/backend/internal/registry/skill.go:23`, regex `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`.

Requested fix: shared slug validation, 128-character limit, inline help/error, upload disabled for invalid names, and focused client/contract tests. A future deployed UI retest is not claimed here.

## Limits

- Only Pi was run through this UI session. Other SDK, identity, fault-soak and concurrency results belong to separate parent acceptance.
- No live viewer login, role switch or RBAC browser test was executed. Contract tests are not live RBAC acceptance.
- Drain/Resume verifies the UI management mutation and restoration on an offline idle registration, not live assignment behavior.
- This intentional cancellation workload created no case or fresh-context dry run; unavailable case evidence is expected.
- The pre-restart run's public historical snapshot has no MCP binding IDs. Actual MCP execution is independently visible in its Trace/evidence.
- Control inputs were observed through actual UI effects; this initial driver revision did not separately log control POSTs. No API-only substitute was used.

## Owned Resources

- Retained conversation: `conv_b0d145f1ffc304b70f29815f258a463f`, UI acceptance run 20261028-212222.
- Deleted empty conversation: `conv_64c48653107c1f0d5c5cadcc346be171`.
- Run: `run_aad2ab9e3a2cea0fe6017388183ee3d6`, cancelled.
- Retained environment: `env_45e1fbad93ca873c44ee8da0ce7d35a6`, ready, generation 2.
- Harness: `ph_1ec0d7980c63d7c2e75235595da685dd`; original version `hv_29b44aa9c65d0f32b8b8e70b6844f1e2`.
- Original digest: `02318e83c95743b3ed82bdbe9cef7cc46e1129c65ff506c546053b65e63c04d1`.
- Published Skill version: `sv_2efa8dcd5620196735a84dd4d2b8b35f`.
- Restored offline runtime: `runtime-KDQDT6WYL9-11619-ULN2EVUU4RR25DOBRHBIM4CJFU`.

## Reuse And Integrity

Driver: `../live-browser.py`, using `worker/.venv/bin/python` and JSON-line UI commands on stdin. It loads credentials programmatically, uses role/label/text locators and real file inputs, and records commands/checkpoints. Generated resource IDs and parent readiness make the audit trail unsuitable for blind replay. The temporary Skill file is recreated by the driver; its exact uploaded content remains in screenshot 06 and report.json.

Screenshots are actual UI captures, center-cropped from 1440x1100 to 1440x1048 to exclude the fixed sidebar account email. The crop removes 26 pixels at top and bottom; no replacement UI pixels were synthesized. Each screenshot's size, dimensions, crop bounds and SHA-256 are in report.json. Visible text is credential-redacted; recorded network metadata contains paths/statuses only, no authorization headers or query tokens.

## Verification

Executed from the repository root:

```bash
worker/.venv/bin/python integration/langfuse-management/live-browser.py --verify integration/langfuse-management/live-browser-evidence-20261028-212222
```

Exit 0: `verified: true`, 61 passed checks, 8 failed checks preserved, 33 screenshots, text secret scan passed. Verification checks each PNG signature, dimensions, bytes and SHA-256; credentials are loaded privately for comparison and never printed. The driver also passed an AST syntax parse. Privacy cropping was visually inspected on the real takeover capture after finalization.
