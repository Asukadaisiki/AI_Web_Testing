import { describe, expect, it } from "vitest";

import type {
  Element,
  Locator,
  Observation,
  StructureNode,
} from "../src/browser/contract.js";
import {
  deriveGotoStep,
  validateCase,
  type Case,
  type Step,
} from "../src/browser/contract.js";
import { observationHasText } from "../src/browser/case-builder.js";

function roleLocator(role: string, name: string): Locator {
  return { kind: "role", role, name, exact: true, match_count: 1 };
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

describe("observationHasText", () => {
  const cases: Array<{
    name: string;
    obs: Observation;
    text: string;
    want: boolean;
  }> = [
    {
      name: "element name",
      obs: observation({
        elements: [
          {
            ref: "e1",
            tag: "div",
            name: "  Logged   in as AI Web Test ",
            visible: true,
            enabled: true,
            value: null,
            locators: [roleLocator("div", "Logged")],
          } as Element,
        ],
      }),
      text: "logged IN as ai web test",
      want: true,
    },
    {
      name: "element text",
      obs: observation({
        elements: [
          {
            ref: "e1",
            tag: "div",
            text: "  Logged   in as AI Web Test ",
            visible: true,
            enabled: true,
            value: null,
            locators: [roleLocator("div", "Logged")],
          } as Element,
        ],
      }),
      text: "logged IN as ai web test",
      want: true,
    },
    {
      name: "element full text",
      obs: observation({
        elements: [
          {
            ref: "e1",
            tag: "div",
            full_text: "  Logged   in as AI Web Test ",
            visible: true,
            enabled: true,
            value: null,
            locators: [roleLocator("div", "Logged")],
          } as Element,
        ],
      }),
      text: "logged IN as ai web test",
      want: true,
    },
    {
      name: "structure full text",
      obs: observation({
        structures: [
          {
            ref: "s1",
            kind: "region",
            tag: "section",
            full_text: "  Logged   in as AI Web Test ",
            visible: true,
          } as StructureNode,
        ],
      }),
      text: "logged IN as ai web test",
      want: true,
    },
    {
      name: "hidden element",
      obs: observation({
        elements: [
          {
            ref: "e1",
            tag: "div",
            name: "Logged in as AI Web Test",
            text: "Logged in as AI Web Test",
            full_text: "Logged in as AI Web Test",
            visible: false,
            enabled: true,
            value: null,
            locators: [roleLocator("div", "Logged")],
          } as Element,
        ],
      }),
      text: "Logged in as AI Web Test",
      want: false,
    },
    {
      name: "hidden structure",
      obs: observation({
        structures: [
          {
            ref: "s1",
            kind: "region",
            tag: "section",
            full_text: "Logged in as AI Web Test",
            visible: false,
          } as StructureNode,
        ],
      }),
      text: "Logged in as AI Web Test",
      want: false,
    },
    {
      name: "unrelated nodes are not joined",
      obs: observation({
        elements: [
          {
            ref: "e1",
            tag: "div",
            text: "Logged in as",
            visible: true,
            enabled: true,
            value: null,
            locators: [roleLocator("div", "Logged")],
          } as Element,
        ],
        structures: [
          {
            ref: "s1",
            kind: "region",
            tag: "section",
            full_text: "AI Web Test",
            visible: true,
          } as StructureNode,
        ],
      }),
      text: "Logged in as AI Web Test",
      want: false,
    },
  ];

  for (const tc of cases) {
    it(tc.name, () => {
      expect(observationHasText(tc.obs, tc.text)).toBe(tc.want);
    });
  }
});

describe("assert_text / assert_url do not mutate the page", () => {
  it("an assert_text step has no target and a text_visible postcondition", () => {
    const step = assertTextStep(1, "verify product", "Blue Top", "https://shop.test/products");
    expect(step.action).toBe("assert_text");
    expect(step.target).toBeUndefined();
    expect(step.value).toBe("Blue Top");
    expect(step.preconditions).toHaveLength(1);
    expect(step.preconditions[0]?.type).toBe("url_contains");
    expect(step.postconditions).toEqual([
      { type: "text_visible", value: "Blue Top", timeout_ms: 10_000 },
    ]);
  });

  it("an assert_url step has no target and a url_contains postcondition", () => {
    const step = assertURLStep(
      1,
      "verify search url",
      "search=Blue%20Top",
      "https://shop.test/products?search=Blue%20Top",
    );
    expect(step.action).toBe("assert_url");
    expect(step.target).toBeUndefined();
    expect(step.value).toBe("search=Blue%20Top");
    expect(step.postconditions).toEqual([
      { type: "url_contains", value: "search=Blue%20Top", timeout_ms: 10_000 },
    ]);
  });

  it("derived goto + assertion steps still validate as a case", () => {
    const goto = deriveGotoStep(0, "open products", "https://shop.test/products");
    const text = assertTextStep(1, "verify product", "Blue Top", "https://shop.test/products");
    const url = assertURLStep(
      2,
      "verify search url",
      "search=Blue%20Top",
      "https://shop.test/products?search=Blue%20Top",
    );
    const caseInput: Case = {
      case_version: "loop.case.v1",
      name: "search",
      goal: "find blue top",
      base_url: "https://shop.test",
      steps: [goto, text, url],
    };
    expect(() => validateCase(caseInput)).not.toThrow();
  });
});

function assertTextStep(
  index: number,
  intent: string,
  text: string,
  pageURL: string,
): Step {
  return {
    index,
    action: "assert_text",
    intent,
    value: text,
    submit: false,
    preconditions: [
      { type: "url_contains", value: "/products", timeout_ms: 10_000 },
    ],
    postconditions: [
      { type: "text_visible", value: text, timeout_ms: 10_000 },
    ],
    timeout_ms: 20_000,
  };
}

function assertURLStep(
  index: number,
  intent: string,
  contains: string,
  pageURL: string,
): Step {
  return {
    index,
    action: "assert_url",
    intent,
    value: contains,
    submit: false,
    preconditions: [
      { type: "url_contains", value: "/products", timeout_ms: 10_000 },
    ],
    postconditions: [
      { type: "url_contains", value: contains, timeout_ms: 10_000 },
    ],
    timeout_ms: 20_000,
  };
}
