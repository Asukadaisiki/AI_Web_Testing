import { describe, expect, it } from "vitest";
import { z } from "zod";

import {
  BROWSER_TOOL_NAMES,
  BROWSER_TOOL_SCHEMAS,
  type BrowserToolName,
} from "../src/mcp/schemas.js";

const EXPECTED_ORDER: BrowserToolName[] = [
  "open_page",
  "click",
  "input",
  "select",
  "check",
  "uncheck",
  "scroll_into_view",
  "hover",
  "dismiss_dialog",
  "upload_file",
  "assert_text",
  "assert_url",
  "assert_element",
  "assert_attribute",
  "assert_count",
  "finish_case",
];

function requiredKeys(shape: Record<string, z.ZodType>): string[] {
  return Object.entries(shape)
    .filter(([, schema]) => !schema.isOptional())
    .map(([key]) => key);
}

function schemaFor(name: BrowserToolName): Record<string, z.ZodType> {
  const schema = BROWSER_TOOL_SCHEMAS.find((item) => item.name === name);
  expect(schema, `schema for ${name} must exist`).toBeDefined();
  return schema!.inputSchema;
}

describe("BROWSER_TOOL_NAMES", () => {
  it("lists the sixteen stable authoring tools in the exact declared order", () => {
    expect([...BROWSER_TOOL_NAMES]).toEqual(EXPECTED_ORDER);
  });

  it("exposes no ask_user tool", () => {
    expect(BROWSER_TOOL_NAMES).not.toContain("ask_user" as BrowserToolName);
  });

  it("contains exactly sixteen entries", () => {
    expect(BROWSER_TOOL_NAMES).toHaveLength(16);
  });
});

describe("BROWSER_TOOL_SCHEMAS order", () => {
  it("follows the same stable order as BROWSER_TOOL_NAMES", () => {
    expect(BROWSER_TOOL_SCHEMAS.map((item) => item.name)).toEqual(
      EXPECTED_ORDER,
    );
  });

  it("every schema carries a non-empty description", () => {
    for (const schema of BROWSER_TOOL_SCHEMAS) {
      expect(schema.description.trim()).not.toBe("");
    }
  });
});

describe("tool argument requirements", () => {
  it("open_page requires url and intent", () => {
    expect(requiredKeys(schemaFor("open_page"))).toEqual(
      expect.arrayContaining(["url", "intent"]),
    );
  });

  it.each([
    "click",
    "select",
    "check",
    "uncheck",
    "scroll_into_view",
    "hover",
    "dismiss_dialog",
    "upload_file",
    "assert_element",
    "assert_attribute",
    "assert_count",
  ] as const)("%s requires intent", (name) => {
    expect(requiredKeys(schemaFor(name))).toContain("intent");
  });

  it.each(["input", "select", "upload_file"] as const)(
    "%s requires value",
    (name) => {
      expect(requiredKeys(schemaFor(name))).toContain("value");
    },
  );

  it("assert_text requires text and intent", () => {
    expect(requiredKeys(schemaFor("assert_text"))).toEqual(
      expect.arrayContaining(["text", "intent"]),
    );
  });

  it("assert_url requires contains and intent", () => {
    expect(requiredKeys(schemaFor("assert_url"))).toEqual(
      expect.arrayContaining(["contains", "intent"]),
    );
  });

  it("assert_element requires intent and expect_element", () => {
    expect(requiredKeys(schemaFor("assert_element"))).toEqual(
      expect.arrayContaining(["intent", "expect_element"]),
    );
  });

  it("assert_attribute requires intent and expect_attribute", () => {
    expect(requiredKeys(schemaFor("assert_attribute"))).toEqual(
      expect.arrayContaining(["intent", "expect_attribute"]),
    );
  });

  it("assert_count requires intent and expect_count", () => {
    expect(requiredKeys(schemaFor("assert_count"))).toEqual(
      expect.arrayContaining(["intent", "expect_count"]),
    );
  });

  it("finish_case requires name", () => {
    expect(requiredKeys(schemaFor("finish_case"))).toContain("name");
  });

  it("target actions expose the expect_* postcondition knobs as optional", () => {
    const action = schemaFor("click");
    for (const key of [
      "expect_text",
      "expect_gone",
      "expect_url",
      "expect_value",
      "expect_element",
      "expect_attribute",
      "expect_count",
    ]) {
      expect(action[key], `click should declare ${key}`).toBeDefined();
      expect(action[key]!.isOptional()).toBe(true);
    }
  });
});
