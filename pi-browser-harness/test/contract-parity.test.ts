import { readFile } from "node:fs/promises";

import { describe, expect, it } from "vitest";

import {
  ACTIONS,
  CASE_ERROR_CODES,
  CASE_VERSION,
  CaseContractError,
  validateCase,
  type ActRequest,
  type ActResponse,
} from "../src/browser/contract.js";

interface FixtureCase {
  name: string;
  expect: { accept: boolean; code?: string };
  case: unknown;
}

interface ContractFixture {
  version: string;
  normalized_before_validation_codes: string[];
  authoring_action: {
    request: ActRequest;
    response: ActResponse;
  };
  cases: FixtureCase[];
}

async function loadFixture(): Promise<ContractFixture> {
  const raw = await readFile(
    new URL("../../fixtures/contract/case_contract.json", import.meta.url),
    "utf8",
  );
  return JSON.parse(raw) as ContractFixture;
}

describe("browser case contract parity", () => {
  it("accepts and rejects the shared fixtures with the stable Go/Python codes", async () => {
    const fixture = await loadFixture();

    expect(fixture.version).toBe(CASE_VERSION);
    expect(fixture.cases.length).toBeGreaterThan(0);

    for (const item of fixture.cases) {
      if (item.expect.accept) {
        expect(() => validateCase(item.case), item.name).not.toThrow();
        continue;
      }

      try {
        validateCase(item.case);
        expect.fail(`${item.name}: expected rejection`);
      } catch (error) {
        expect(error, item.name).toBeInstanceOf(CaseContractError);
        expect((error as CaseContractError).code, item.name).toBe(
          item.expect.code,
        );
      }
    }
  });

  it("normalizes the same defaults and indices as Go and Python", () => {
    const normalized = validateCase({
      name: "normalized",
      goal: "prove defaults",
      base_url: "https://shop.test",
      steps: [
        {
          index: 42,
          action: "goto",
          intent: "open",
          value: "https://shop.test/products",
          postconditions: [{ type: "url_contains", value: "/products" }],
        },
      ],
    });

    expect(normalized).toMatchObject({
      case_version: "loop.case.v1",
      steps: [
        {
          index: 0,
          preconditions: [],
          timeout_ms: 20_000,
          postconditions: [{ timeout_ms: 10_000 }],
        },
      ],
    });
  });

  it("keeps the shared fixture exhaustive over actions and stable codes", async () => {
    const fixture = await loadFixture();
    const coveredActions = new Set<string>();
    const coveredCodes = new Set<string>(
      fixture.normalized_before_validation_codes,
    );

    for (const item of fixture.cases) {
      if (item.expect.code !== undefined) {
        coveredCodes.add(item.expect.code);
      }
      if (
        typeof item.case === "object" &&
        item.case !== null &&
        "steps" in item.case &&
        Array.isArray(item.case.steps)
      ) {
        for (const step of item.case.steps) {
          if (
            typeof step === "object" &&
            step !== null &&
            "action" in step &&
            typeof step.action === "string"
          ) {
            coveredActions.add(step.action);
          }
        }
      }
    }

    expect(ACTIONS.every((action) => coveredActions.has(action))).toBe(true);
    expect(CASE_ERROR_CODES.every((code) => coveredCodes.has(code))).toBe(true);
  });

  it("types the retained authoring request and response fixture", async () => {
    const fixture = await loadFixture();

    expect(fixture.authoring_action.request.action).toBe("assert_element");
    expect(fixture.authoring_action.response.status).toBe("failed");
    expect(fixture.authoring_action.response.error?.kind).toBe(
      "condition_unmet",
    );
  });
});
