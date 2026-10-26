import { describe, expect, it } from "vitest";

import type {
  Action,
  ActionCandidate,
  Element,
  Locator,
  Observation,
  StructureNode,
  TargetSpec,
} from "../src/browser/contract.js";
import {
  GroundingError,
  resolveTarget,
  type TargetRequest,
} from "../src/browser/grounding.js";

function roleLocator(role: string, name: string): Locator {
  return { kind: "role", role, name, exact: true, match_count: 1 };
}

function cssLocator(css: string): Locator {
  return { kind: "css", css, match_count: 1 };
}

function element(
  ref: string,
  overrides: Partial<Element> = {},
): Element {
  return {
    ref,
    tag: "div",
    role: "button",
    name: ref,
    text: ref,
    value: null,
    visible: true,
    enabled: true,
    locators: [roleLocator("button", ref)],
    ...overrides,
  };
}

function structure(
  ref: string,
  overrides: Partial<StructureNode> = {},
): StructureNode {
  return {
    ref,
    kind: "section",
    tag: "section",
    full_text: "",
    visible: true,
    ...overrides,
  };
}

function observation(
  overrides: Partial<Observation> = {},
): Observation {
  return {
    observation_id: "obs_current",
    page_state_id: "page_current",
    browser_session_id: "browser_current",
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

function expectCode(
  request: TargetRequest,
  current: Observation,
  code: GroundingError["code"],
): GroundingError {
  try {
    resolveTarget(request, current);
  } catch (error) {
    expect(error).toBeInstanceOf(GroundingError);
    expect((error as GroundingError).code).toBe(code);
    return error as GroundingError;
  }
  throw new Error(`expected ${code}`);
}

describe("resolveTarget", () => {
  it("prefers an exact accessible name and normalizes case and whitespace", () => {
    const exactLocator = roleLocator("button", "Add");
    const current = observation({
      elements: [
        element("long", {
          name: "Add to cart",
          text: "Add",
          locators: [roleLocator("button", "Add to cart")],
        }),
        element("exact", {
          name: "Add",
          text: "Add",
          locators: [exactLocator],
        }),
      ],
    });

    const grounded = resolveTarget(
      { action: "click", hint: "  ADD \n" },
      current,
    );

    expect(grounded.element.ref).toBe("exact");
    expect(grounded.locator).toBe(exactLocator);
    expect(grounded.candidate_id).toBe("exact:0");
    expect(grounded.grounding).toEqual({
      observation_id: "obs_current",
      page_state_id: "page_current",
      candidate_id: "exact:0",
      page_url: "https://shop.test/products",
    });
  });

  it.each([
    {
      name: "empty hint",
      request: { action: "click", hint: "  " } satisfies TargetRequest,
      current: observation(),
      code: "target_hint_empty",
    },
    {
      name: "unknown hint",
      request: { action: "click", hint: "Buy now" } satisfies TargetRequest,
      current: observation({
        elements: [element("Checkout", { name: "Checkout" })],
      }),
      code: "target_not_found",
    },
    {
      name: "ambiguous hint",
      request: { action: "click", hint: "Widget" } satisfies TargetRequest,
      current: observation({
        elements: [
          element("first", { name: "Widget", text: "Widget" }),
          element("second", { name: "Widget", text: "Widget" }),
        ],
      }),
      code: "target_ambiguous",
    },
  ] as const)("returns $code for $name", ({ request, current, code }) => {
    const error = expectCode(request, current, code);
    if (code !== "target_hint_empty") {
      expect(error.candidates.length).toBeGreaterThan(0);
    }
  });

  it("ignores hidden, disabled, and unverified elements", () => {
    const verifiedLocator = roleLocator("button", "Submit");
    const current = observation({
      elements: [
        element("hidden", { name: "Submit", visible: false }),
        element("disabled", { name: "Submit", enabled: false }),
        element("unverified", { name: "Submit", locators: [] }),
        element("usable", {
          name: "Submit",
          text: "Submit",
          locators: [verifiedLocator],
        }),
      ],
    });

    const grounded = resolveTarget(
      { action: "click", hint: "Submit" },
      current,
    );

    expect(grounded.element.ref).toBe("usable");
    expect(grounded.locator).toBe(verifiedLocator);
  });

  it("scopes duplicate links to the matching structure and descendants", () => {
    const blueLocator = cssLocator("#blue-link");
    const spec: TargetSpec = {
      object: { role: "link", text: "View Product" },
      scope: { kind: "card", contains_text: "Blue Top" },
    };
    const current = observation({
      structures: [
        structure("blue", {
          kind: "card",
          full_text: "Blue Top",
        }),
        structure("blue-actions", {
          kind: "group",
          parent_ref: "blue",
          full_text: "View Product",
        }),
        structure("red", {
          kind: "card",
          full_text: "Red Top View Product",
        }),
      ],
      elements: [
        element("blue-link", {
          role: "link",
          name: "",
          text: "View Product",
          container_ref: "blue-actions",
          locators: [blueLocator],
        }),
        element("red-link", {
          role: "link",
          name: "",
          text: "View Product",
          container_ref: "red",
          locators: [cssLocator("#red-link")],
        }),
      ],
    });

    const grounded = resolveTarget({ action: "click", spec }, current);

    expect(grounded.element.ref).toBe("blue-link");
    expect(grounded.locator).toBe(blueLocator);
  });

  it("matches aliases against stable attributes inside a scope", () => {
    const submitLocator = cssLocator("#submit_search");
    const current = observation({
      structures: [
        structure("search-form", {
          kind: "form",
          full_text: "Search items",
        }),
      ],
      elements: [
        element("submit", {
          name: "",
          text: "\uf002",
          container_ref: "search-form",
          attributes: { id: "submit_search", type: "button" },
          locators: [submitLocator],
        }),
      ],
    });

    const grounded = resolveTarget(
      {
        action: "click",
        spec: {
          object: { aliases: ["search submit", "submit_search"] },
          scope: { kind: "form", contains_text: "Search items" },
        },
      },
      current,
    );

    expect(grounded.element.ref).toBe("submit");
    expect(grounded.locator).toBe(submitLocator);
  });

  it("returns scope_not_found without a matching structure", () => {
    const error = expectCode(
      {
        action: "click",
        spec: {
          object: { role: "button", name: "Save" },
          scope: { kind: "dialog", contains_text: "Settings" },
        },
      },
      observation({ structures: [structure("products")] }),
      "scope_not_found",
    );

    expect(error.candidates[0]?.ref).toBe("products");
  });

  it("grounds an advertised candidate only for its action and verified locator", () => {
    const verifiedLocator = cssLocator("#submit_search");
    const target = element("submit", {
      locators: [verifiedLocator],
    });
    const candidate: ActionCandidate = {
      candidate_id: "candidate-submit",
      kind: "form_submit_candidate",
      action: "click",
      target_ref: "submit",
      locator: { ...verifiedLocator },
      confidence: "high",
    };
    const current = observation({
      elements: [target],
      action_candidates: [candidate],
    });

    const grounded = resolveTarget(
      { action: "click", candidate_id: "candidate-submit" },
      current,
    );

    expect(grounded.element).toBe(target);
    expect(grounded.locator).toBe(verifiedLocator);
    expect(grounded.candidate_id).toBe("candidate-submit");
  });

  it.each([
    {
      name: "stale id",
      action: "click",
      candidateId: "candidate-old",
      candidateAction: "click",
      candidateLocator: cssLocator("#submit"),
      code: "candidate_not_found",
      includeCandidate: false,
    },
    {
      name: "incompatible action",
      action: "input",
      candidateId: "candidate-submit",
      candidateAction: "click",
      candidateLocator: cssLocator("#submit"),
      code: "candidate_incompatible",
      includeCandidate: true,
    },
    {
      name: "unverified locator",
      action: "click",
      candidateId: "candidate-submit",
      candidateAction: "click",
      candidateLocator: cssLocator("#different"),
      code: "target_not_found",
      includeCandidate: true,
    },
  ] as const)(
    "rejects $name with $code",
    ({
      action,
      candidateId,
      candidateAction,
      candidateLocator,
      code,
      includeCandidate,
    }) => {
      const advertised: ActionCandidate = {
        candidate_id: "candidate-submit",
        kind: "form_submit_candidate",
        action: candidateAction,
        target_ref: "submit",
        locator: candidateLocator,
        confidence: "high",
      };
      expectCode(
        { action: action as Action, candidate_id: candidateId },
        observation({
          elements: [
            element("submit", { locators: [cssLocator("#submit")] }),
          ],
          action_candidates: includeCandidate ? [advertised] : [],
        }),
        code,
      );
    },
  );

  it("falls back to exactly one material semantic candidate", () => {
    const locator = cssLocator("#submit_search");
    const current = observation({
      structures: [structure("products", { full_text: "Products" })],
      elements: [
        element("search-submit", {
          name: "\uf002",
          text: "\uf002",
          attributes: { id: "submit_search" },
          locators: [locator],
        }),
      ],
      action_candidates: [
        {
          candidate_id: "candidate-search-submit",
          kind: "form_submit_candidate",
          action: "click",
          target_ref: "search-submit",
          role: "button",
          aliases: ["form submit", "submit_search", "submit search"],
          locator: { ...locator },
          confidence: "high",
        },
      ],
    });

    const grounded = resolveTarget(
      {
        action: "click",
        hint: "submit search",
        spec: {
          object: { role: "button", aliases: ["submit search"] },
          scope: { kind: "form", ref: "missing-form" },
        },
      },
      current,
    );

    expect(grounded.element.ref).toBe("search-submit");
    expect(grounded.locator).toBe(locator);
    expect(grounded.candidate_id).toBe("candidate-search-submit");
  });

  it("preserves the original error when multiple material candidates match", () => {
    const firstLocator = cssLocator("#first-submit");
    const secondLocator = cssLocator("#second-submit");
    const current = observation({
      structures: [structure("products")],
      elements: [
        element("first", { locators: [firstLocator] }),
        element("second", { locators: [secondLocator] }),
      ],
      action_candidates: [
        {
          candidate_id: "first-submit",
          kind: "control",
          action: "click",
          target_ref: "first",
          role: "button",
          aliases: ["submit search"],
          locator: { ...firstLocator },
          confidence: "high",
        },
        {
          candidate_id: "second-submit",
          kind: "control",
          action: "click",
          target_ref: "second",
          role: "button",
          aliases: ["submit search control"],
          locator: { ...secondLocator },
          confidence: "medium",
        },
      ],
    });

    expectCode(
      {
        action: "click",
        hint: "submit search",
        spec: {
          object: { role: "button", name: "submit search" },
          scope: { kind: "form", ref: "missing-form" },
        },
      },
      current,
      "scope_not_found",
    );
  });
});
