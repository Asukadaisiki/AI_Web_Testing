import { createHash } from "node:crypto";

import type { Action, Locator, Observation } from "./contract.js";

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
    targetKey: string,
    errorCode: string,
  ): void {
    this.remember({
      page_fingerprint: pageFingerprint(observation),
      action,
      target_key: normalizeTargetKey(targetKey),
      error_code: errorCode,
    });
  }

  failed(
    observation: Observation,
    action: Action,
    targetKey: string,
  ): boolean {
    const fingerprint = pageFingerprint(observation);
    const normalizedTarget = normalizeTargetKey(targetKey);
    return this.#signatures.some(
      (signature) =>
        signature.page_fingerprint === fingerprint &&
        signature.action === action &&
        signature.target_key === normalizedTarget,
    );
  }

  rejectRepeated(
    observation: Observation,
    action: Action,
    targetKey: string,
  ): void {
    if (this.failed(observation, action, targetKey)) {
      throw new StrategyRepeatedError();
    }
  }
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

function normalizeTargetKey(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}
