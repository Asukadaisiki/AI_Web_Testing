import { describe, expect, it } from "vitest";

import type {
  ActionCandidate,
  Blocker,
  Element,
  Locator,
  Observation,
  StructureNode,
} from "../src/browser/contract.js";
import {
  buildPageView,
  type CompactResult,
} from "../src/browser/observation-view.js";

function cssLocator(css: string): Locator {
  return { kind: "css", css, match_count: 1 };
}

function element(ref: string, name = ref): Element {
  return {
    ref,
    tag: "button",
    role: "button",
    name,
    text: name,
    value: null,
    visible: true,
    enabled: true,
    locators: [cssLocator(`[data-ref="${ref}"]`)],
    visible_in_viewport: true,
  };
}

function candidate(ref: string, name = ref): ActionCandidate {
  return {
    candidate_id: `candidate-${ref}`,
    kind: "element_candidate",
    action: "click",
    target_ref: ref,
    role: "button",
    name,
    text: name,
    aliases: [name],
    locator: cssLocator(`[data-ref="${ref}"]`),
    confidence: "high",
  };
}

function scope(ref: string): StructureNode {
  return {
    ref,
    kind: "section",
    tag: "section",
    full_text: ref,
    visible: true,
  };
}

function blocker(ref: string): Blocker {
  return {
    kind: "overlay",
    ref,
    confidence: "medium",
    reason: ref,
  };
}

function observation(
  overrides: Partial<Observation> = {},
): Observation {
  return {
    observation_id: "obs",
    page_state_id: "page",
    browser_session_id: "browser",
    url: "https://app.test/page",
    title: "Page",
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

describe("buildPageView", () => {
  it("enforces all compact bounds and reports stable omitted counts", () => {
    const current = observation({
      elements: Array.from({ length: 23 }, (_, index) =>
        element(`element-${String(index).padStart(2, "0")}`),
      ).reverse(),
      action_candidates: Array.from({ length: 15 }, (_, index) =>
        candidate(`element-${String(index).padStart(2, "0")}`),
      ).reverse(),
      structures: Array.from({ length: 11 }, (_, index) =>
        scope(`scope-${String(index).padStart(2, "0")}`),
      ).reverse(),
      blockers: Array.from({ length: 7 }, (_, index) =>
        blocker(`blocker-${String(index).padStart(2, "0")}`),
      ).reverse(),
    });

    const view = buildPageView(current, "", null);

    expect(view.elements).toHaveLength(20);
    expect(view.action_candidates).toHaveLength(12);
    expect(view.scopes).toHaveLength(8);
    expect(view.blockers).toHaveLength(5);
    expect(view.truncated).toBe(true);
    expect(view.truncation_reason).toBe(
      "page_view_omitted blockers=2 action_candidates=3 scopes=3 elements=3",
    );
    expect(view.elements[0]?.ref).toBe("element-00");
    expect(view.action_candidates[0]?.candidate_id).toBe(
      "candidate-element-00",
    );
    expect(view.scopes[0]?.ref).toBe("scope-00");
    expect(view.blockers[0]?.ref).toBe("blocker-00");
  });

  it("retains a goal-relevant candidate, target, and nested scope ahead of ads", () => {
    const ads = Array.from({ length: 31 }, (_, index) => {
      const ref = `ad-${String(index).padStart(2, "0")}`;
      return { element: element(ref, "Sponsored offer"), candidate: candidate(ref, "Sponsored offer") };
    });
    const target = element("search-submit", "");
    target.container_ref = "search-controls";
    const current = observation({
      structures: [
        {
          ...scope("search-form"),
          kind: "form",
          full_text: "Search products",
        },
        {
          ...scope("search-controls"),
          kind: "group",
          parent_ref: "search-form",
          full_text: "Controls",
        },
      ],
      elements: [...ads.map((item) => item.element), target],
      action_candidates: [
        ...ads.map((item) => item.candidate),
        {
          ...candidate("search-submit", ""),
          candidate_id: "candidate-search-submit",
          kind: "form_submit_candidate",
          aliases: ["search submit"],
          relations: [
            { type: "form_submit_candidate", ref: "search-form" },
          ],
        },
      ],
    });

    const view = buildPageView(
      current,
      "search products and submit the search form",
      null,
    );

    expect(view.action_candidates[0]?.candidate_id).toBe(
      "candidate-search-submit",
    );
    expect(view.elements.some((item) => item.ref === "search-submit")).toBe(
      true,
    );
    expect(view.scopes.some((item) => item.ref === "search-controls")).toBe(
      true,
    );
  });

  it("retains entries related to the latest result ahead of unrelated controls", () => {
    const unrelated = Array.from({ length: 24 }, (_, index) =>
      element(`unrelated-${String(index).padStart(2, "0")}`, "Advertisement"),
    );
    const retry = element("retry-target", "Retry checkout");
    const current = observation({
      elements: [...unrelated, retry],
      action_candidates: [
        ...unrelated.map((item) => candidate(item.ref, "Advertisement")),
        candidate("retry-target", "Retry checkout"),
      ],
    });
    const lastResult: CompactResult = {
      ok: false,
      failure: {
        steps: [
          {
            action: "click",
            hit_test: {
              target_ref: "retry-target",
              x: 10,
              y: 20,
              hit_ref: null,
              hit_tag: null,
              covered: false,
              blocker_kind: null,
            },
          },
        ],
      },
    };

    const view = buildPageView(current, "finish purchase", lastResult);

    expect(view.action_candidates[0]?.candidate_id).toBe(
      "candidate-retry-target",
    );
    expect(view.elements[0]?.ref).toBe("retry-target");
  });

  it("preserves source truncation and omits raw candidate locators", () => {
    const current = observation({
      truncated: true,
      truncation_reason: "worker_max_elements",
      elements: [element("submit", "Submit")],
      action_candidates: [candidate("submit", "Submit")],
    });

    const view = buildPageView(current, "", null);

    expect(view.truncated).toBe(true);
    expect(view.truncation_reason).toBe("worker_max_elements");
    expect(view.action_candidates[0]).not.toHaveProperty("locator");
    expect(view.elements[0]?.locator_kind).toBe("css");
  });

  it("matches Chinese, Japanese, and Korean goal text", () => {
    const cases = [
      ["搜索蓝色上衣并加入购物车", "加入购物车"],
      ["商品かなカナ追加", "なカ"],
      ["장바구니상품추가", "상품추가"],
    ] as const;

    for (const [goal, label] of cases) {
      const ads = Array.from({ length: 20 }, (_, index) =>
        element(`ad-${index}`, "Sponsored offer"),
      );
      const target = element(`target-${label}`, label);
      const view = buildPageView(
        observation({
          elements: [...ads, target],
          action_candidates: [
            ...ads.map((item) => candidate(item.ref, "Sponsored offer")),
            candidate(target.ref, label),
          ],
        }),
        goal,
        null,
      );

      expect(
        view.action_candidates.some(
          (item) => item.candidate_id === `candidate-${target.ref}`,
        ),
      ).toBe(true);
    }
  });
});
