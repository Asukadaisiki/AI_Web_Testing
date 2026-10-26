---
name: browser-case-authoring
description: Strategy for authoring one executable browser case through the browser tools, from candidate selection through dry-run repair and final assertion completeness.
---

# Browser Case Authoring

Author one executable browser case by driving the page through the browser tools and finishing with `finish_case`. Follow this strategy for every run.

## 1. Candidate selection and semantic grounding

- Prefer an `action_candidate` from the latest page view whose advertised action exactly matches the tool you are calling. Pass its `candidate_id` instead of describing a low-semantics target.
- Never reuse a candidate advertised for another action. Assertion tools must not reuse click or input candidates.
- If no compatible candidate exists, provide a `target` (object/scope/relation) or a `hint` grounded in current visible elements. Never pass CSS or XPath.
- Resolve every target against the latest observation only. After any action, re-read the updated page view before choosing the next target.

## 2. Blocker handling

- If the page view reports a blocker (modal, overlay, login wall, CAPTCHA), address it only when you can dismiss it legitimately with the available tools.
- If a blocker requires credentials, a challenge, or an action the tools cannot perform, stop and report the blocker rather than bypassing it.
- Dismiss dialogs and overlays with `dismiss_dialog` when a close control is available, then re-observe before continuing.

## 3. Repair after an authoring action fails

- When a tool returns `ok: false` with a stable error code, read the `detail` and the latest `page` to understand the failure.
- For grounding failures (`target_not_found`, ambiguous matches), refine the target using current elements or pick a different candidate. Do not repeat the exact same call.
- For action failures, adjust the expectation or the target rather than retrying blindly. The failure ledger rejects repeated identical strategies.
- Each repair attempt must include an objective expectation so the repaired step is verifiable.

## 4. Repair after a fresh dry run fails

- `finish_case` runs the case in a fresh browser context. If the dry run fails, the result includes the failing steps.
- Replay only against the reported failing steps. Fix the target, value, or expectation that failed, then call `finish_case` again.
- A passing dry run is required for a successful result. Do not finish or claim success while the latest dry run is failing.

## 5. Final assertion completeness

- Before calling `finish_case`, inspect the committed steps and confirm every explicit expected outcome from the user's goal has committed proof on the relevant final state.
- Navigation or current visibility alone is not proof. Use an assertion tool or an action postcondition that explicitly encodes the expected outcome.
- Setting an input earlier does not prove its value persisted on a later or final page. Assert final requested values and counts where they matter.
- The backend validates the case; an incomplete or unproven case fails the dry run.

## 6. Minimal-case guidance

- Keep the case minimal: only the steps needed to reach and verify the goal. Do not add exploratory or redundant steps.
- Use `intent` on every step to record why it exists, in the user's language.
- Name the case concisely in `finish_case`. The case is stored and sent for approval only after a passing dry run.
