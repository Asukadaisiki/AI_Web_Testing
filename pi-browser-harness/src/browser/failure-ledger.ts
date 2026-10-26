import { createHash } from "node:crypto";

import type { Action, Locator, Observation, TargetSpec } from "./contract.js";

export const STRATEGY_REPEATED = "strategy_repeated" as const;
const MAX_FAILURE_SIGNATURES = 8;
const STRATEGY_REPEATED_DETAIL =
  "this action and target already failed on the unchanged page; change target, scope, action, or page state";

export interface FailureSignature {
  page_fingerprint: string;
  action: Action;
  target_key: string;
  error_code: string;
}

interface FingerprintControlValue {
  key: string;
  value: string;
}

interface PageFingerprintPayload {
  url: string;
  title: string;
  action_candidate_ids: string[] | null;
  control_values: FingerprintControlValue[] | null;
  blocker_kinds: string[] | null;
}

export class StrategyRepeatedError extends Error {
  readonly code = STRATEGY_REPEATED;

  constructor() {
    super(STRATEGY_REPEATED_DETAIL);
    this.name = "StrategyRepeatedError";
  }
}

export function pageFingerprint(observation: Observation): string {
  const candidateIDs = sortedUnique(
    observation.action_candidates
      .map((candidate) => candidate.candidate_id.trim())
      .filter(Boolean),
  );
  const controlValues = observation.elements
    .filter(
      (element) =>
        element.visible &&
        element.value !== null &&
        element.locators.length > 0,
    )
    .map((element) => ({
      key: canonicalLocator(element.locators[0]!),
      value: element.value!,
    }))
    .sort(
      (left, right) =>
        left.key.localeCompare(right.key) ||
        left.value.localeCompare(right.value),
    );
  const blockerKinds = sortedUnique(
    observation.blockers
      .map((blocker) => canonicalText(blocker.kind))
      .filter(Boolean),
  );
  const payload: PageFingerprintPayload = {
    url: canonicalPageURL(observation.url),
    title: canonicalText(observation.title),
    action_candidate_ids: candidateIDs.length === 0 ? null : candidateIDs,
    control_values: controlValues.length === 0 ? null : controlValues,
    blocker_kinds: blockerKinds.length === 0 ? null : blockerKinds,
  };
  return createHash("sha256").update(JSON.stringify(payload)).digest("hex");
}

export class FailureLedger {
  #signatures: FailureSignature[] = [];

  get signatures(): readonly FailureSignature[] {
    return this.#signatures.map((signature) => ({ ...signature }));
  }

  remember(signature: FailureSignature): void {
    this.#signatures = this.#signatures.filter(
      (existing) => !sameSignature(existing, signature),
    );
    this.#signatures.push({ ...signature });
    if (this.#signatures.length > MAX_FAILURE_SIGNATURES) {
      this.#signatures = this.#signatures.slice(-MAX_FAILURE_SIGNATURES);
    }
  }

  record(
    observation: Observation,
    action: Action,
    errorCode: string,
    candidateId?: string,
    spec?: TargetSpec,
    hint?: string,
  ): void {
    this.remember({
      page_fingerprint: pageFingerprint(observation),
      action,
      target_key: failureTargetKey(candidateId, spec, hint),
      error_code: errorCode,
    });
  }

  failed(
    observation: Observation,
    action: Action,
    candidateId?: string,
    spec?: TargetSpec,
    hint?: string,
  ): boolean {
    const fingerprint = pageFingerprint(observation);
    const targetKey = failureTargetKey(candidateId, spec, hint);
    return this.#signatures.some(
      (signature) =>
        signature.page_fingerprint === fingerprint &&
        signature.action === action &&
        signature.target_key === targetKey,
    );
  }

  rejectRepeated(
    observation: Observation,
    action: Action,
    candidateId?: string,
    spec?: TargetSpec,
    hint?: string,
  ): void {
    if (this.failed(observation, action, candidateId, spec, hint)) {
      throw new StrategyRepeatedError();
    }
  }
}

/**
 * Computes a canonical failure target key from a candidate ID, structured
 * target spec, or free-form hint.  Aligns with the v2 planner
 * `failureTargetKey`: the candidate ID is compared case-sensitively (trim
 * only), while spec fields and hints are case-folded and whitespace-normalized
 * before JSON serialization.
 */
export function failureTargetKey(
  candidateId?: string,
  spec?: TargetSpec,
  hint?: string,
): string {
  const candidate = (candidateId ?? "").trim();
  if (candidate !== "") {
    return candidate;
  }
  if (!spec) {
    return normalize(hint ?? "");
  }

  const object = { ...spec.object };
  if (!object.role) object.role = spec.role ?? "";
  if (!object.text) object.text = spec.text ?? "";
  if (!object.name) object.name = spec.name ?? "";
  if (!object.aliases || object.aliases.length === 0) {
    object.aliases = spec.aliases ?? [];
  }

  const normalizedObject: Record<string, unknown> = {};
  const role = normalize(object.role ?? "");
  if (role) normalizedObject.role = role;
  const text = normalize(object.text ?? "");
  if (text) normalizedObject.text = text;
  const name = normalize(object.name ?? "");
  if (name) normalizedObject.name = name;
  const aliases = normalizedStrings(object.aliases ?? []);
  if (aliases.length > 0) normalizedObject.aliases = aliases;

  const payload: Record<string, unknown> = { object: normalizedObject };

  if (spec.scope) {
    const scope: Record<string, unknown> = {};
    const kind = normalize(spec.scope.kind ?? "");
    if (kind) scope.kind = kind;
    const containsText = normalize(spec.scope.contains_text ?? "");
    if (containsText) scope.contains_text = containsText;
    const ref = (spec.scope.ref ?? "").trim();
    if (ref) scope.ref = ref;
    payload.scope = scope;
  }

  const relation = normalize(spec.relation ?? "");
  if (relation) payload.relation = relation;

  return JSON.stringify(payload);
}

function sameSignature(
  left: FailureSignature,
  right: FailureSignature,
): boolean {
  return (
    left.page_fingerprint === right.page_fingerprint &&
    left.action === right.action &&
    left.target_key === right.target_key &&
    left.error_code === right.error_code
  );
}

function canonicalLocator(locator: Locator): string {
  return JSON.stringify({
    kind: locator.kind,
    ...(locator.role ? { role: locator.role } : {}),
    ...(locator.name ? { name: locator.name } : {}),
    ...(locator.exact ? { exact: locator.exact } : {}),
    ...(locator.text ? { text: locator.text } : {}),
    ...(locator.css ? { css: locator.css } : {}),
  });
}

function canonicalPageURL(raw: string): string {
  const value = raw.trim();
  try {
    const parsed = new URL(value);
    parsed.protocol = parsed.protocol.toLowerCase();
    parsed.hostname = parsed.hostname.toLowerCase();
    if (
      (parsed.protocol === "https:" && parsed.port === "443") ||
      (parsed.protocol === "http:" && parsed.port === "80")
    ) {
      parsed.port = "";
    }
    if (parsed.pathname === "") {
      parsed.pathname = "/";
    }
    parsed.searchParams.sort();
    return parsed.toString();
  } catch {
    return value;
  }
}

function canonicalText(value: string): string {
  return value.trim().split(/\s+/u).filter(Boolean).join(" ");
}

function sortedUnique(values: string[]): string[] {
  return [...new Set(values)].sort();
}

function normalize(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}

function normalizedStrings(values: string[]): string[] {
  return sortedUnique(values.map(normalize).filter(Boolean));
}
