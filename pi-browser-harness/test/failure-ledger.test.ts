import { describe, expect, it } from "vitest";

import type {
  Element,
  Locator,
  Observation,
  TargetSpec,
} from "../src/browser/contract.js";
import {
  FailureLedger,
  StrategyRepeatedError,
  failureTargetKey,
  pageFingerprint,
} from "../src/browser/failure-ledger.js";

function roleLocator(matchCount: number): Locator {
  return {
    kind: "role",
    role: "searchbox",
    name: "Search",
    exact: true,
    match_count: matchCount,
  };
}

function control(
  ref: string,
  value: string,
  matchCount: number,
): Element {
  return {
    ref,
    tag: "input",
    role: "searchbox",
    name: "Search",
    text: null,
    value,
    visible: true,
    enabled: true,
    locators: [roleLocator(matchCount)],
  };
}

function observation(
  overrides: Partial<Observation> = {},
): Observation {
  return {
    observation_id: "obs",
    page_state_id: "page",
    browser_session_id: "browser",
    url: "https://shop.test/products",
    title: "Products",
    elements: [],
    structures: [],
    blockers: [],
    action_candidates: [],
    truncated: false,
    truncation_reason: null,
    screenshot_path: null,
    ...overrides,
  };
}

describe("pageFingerprint", () => {
  it("canonicalizes replay-equivalent pages", () => {
    const first = observation({
      observation_id: "obs_random_1",
      page_state_id: "page_random_1",
      url: " HTTPS://SHOP.TEST:443/products?b=2&a=1#results ",
      title: "  All   Products ",
      action_candidates: [
        {
          candidate_id: "candidate-b",
          kind: "control",
          action: "click",
          target_ref: "search",
          locator: roleLocator(1),
          confidence: "high",
        },
        {
          candidate_id: "candidate-a",
          kind: "control",
          action: "click",
          target_ref: "search",
          locator: roleLocator(1),
          confidence: "high",
        },
      ],
      elements: [
        {
          ...control("hidden", "ignored", 1),
          visible: false,
        },
        control("control-17", "widget", 1),
      ],
      blockers: [
        {
          kind: "blocked_by_overlay",
          confidence: "high",
          reason: "overlay",
        },
        {
          kind: "blocked_by_dialog",
          confidence: "high",
          reason: "dialog",
        },
      ],
    });
    const replayed = observation({
      observation_id: "obs_random_2",
      page_state_id: "page_random_2",
      url: "https://shop.test/products?a=1&b=2#results",
      title: "All Products",
      action_candidates: [...first.action_candidates].reverse(),
      elements: [
        control("control-93", "widget", 7),
        {
          ...control("hidden", "changed but hidden", 1),
          visible: false,
        },
      ],
      blockers: [...first.blockers].reverse(),
    });

    expect(pageFingerprint(first)).toMatch(/^[a-f0-9]{64}$/);
    expect(pageFingerprint(replayed)).toBe(pageFingerprint(first));
  });

  it("changes for URL fragments and visible control values", () => {
    const before = observation({
      url: "https://shop.test/products#list",
      elements: [control("search", "widget", 1)],
    });

    expect(
      pageFingerprint({ ...before, url: "https://shop.test/products#details" }),
    ).not.toBe(pageFingerprint(before));
    expect(
      pageFingerprint({
        ...before,
        elements: [control("search", "blue top", 1)],
      }),
    ).not.toBe(pageFingerprint(before));
  });
});

describe("FailureLedger", () => {
  it("retains the newest eight unique signatures and moves duplicates newest", () => {
    const ledger = new FailureLedger();
    for (let index = 0; index < 10; index += 1) {
      ledger.remember({
        page_fingerprint: `page-${index}`,
        action: "click",
        target_key: "candidate",
        error_code: "condition_unmet",
      });
    }

    expect(ledger.signatures).toHaveLength(8);
    expect(ledger.signatures[0]?.page_fingerprint).toBe("page-2");
    expect(ledger.signatures[7]?.page_fingerprint).toBe("page-9");

    const duplicate = ledger.signatures[3]!;
    ledger.remember(duplicate);
    expect(ledger.signatures).toHaveLength(8);
    expect(ledger.signatures[7]).toEqual(duplicate);
  });

  it("rejects a repeated action and normalized target on an unchanged page", () => {
    const current = observation({
      elements: [control("search", "widget", 1)],
    });
    const ledger = new FailureLedger();
    ledger.record(current, "click", "condition_unmet", undefined, undefined, "  WIDGET \n");

    expect(() =>
      ledger.rejectRepeated(current, "click", undefined, undefined, "widget"),
    ).toThrow(StrategyRepeatedError);
    try {
      ledger.rejectRepeated(current, "click", undefined, undefined, "widget");
    } catch (error) {
      expect((error as StrategyRepeatedError).code).toBe(
        "strategy_repeated",
      );
    }
  });

  it("allows the same strategy after a semantic page change", () => {
    const before = observation({
      elements: [control("search", "before", 1)],
    });
    const after = observation({
      elements: [control("search", "after", 1)],
    });
    const ledger = new FailureLedger();
    ledger.record(before, "click", "condition_unmet", undefined, undefined, "widget");

    expect(() =>
      ledger.rejectRepeated(after, "click", undefined, undefined, "widget"),
    ).not.toThrow();
  });

  it("treats structurally equivalent specs as the same target key", () => {
    const specInline: TargetSpec = {
      object: { role: "Button", text: " Submit " },
    };
    const specTopLevel: TargetSpec = {
      object: {},
      role: "  BUTTON ",
      text: "SUBMIT",
    };

    expect(failureTargetKey(undefined, specInline)).toBe(
      failureTargetKey(undefined, specTopLevel),
    );

    const current = observation({
      elements: [control("search", "widget", 1)],
    });
    const ledger = new FailureLedger();
    ledger.record(current, "click", "condition_unmet", undefined, specInline);

    expect(() =>
      ledger.rejectRepeated(current, "click", undefined, specTopLevel),
    ).toThrow(StrategyRepeatedError);
  });

  it("preserves candidate ID case so differently-cased IDs are distinct", () => {
    const current = observation({
      elements: [control("search", "widget", 1)],
    });
    const ledger = new FailureLedger();
    ledger.record(current, "click", "condition_unmet", "Candidate-A");

    expect(() =>
      ledger.rejectRepeated(current, "click", "Candidate-A"),
    ).toThrow(StrategyRepeatedError);
    expect(() =>
      ledger.rejectRepeated(current, "click", "candidate-a"),
    ).not.toThrow();
  });
});
