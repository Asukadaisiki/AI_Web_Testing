import { z } from "zod";

/**
 * The sixteen stable browser authoring tools exposed to the planner over MCP.
 * `ask_user` is intentionally handled by the runtime outside this tool set.
 */
export const BROWSER_TOOL_NAMES = [
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
] as const;

export type BrowserToolName = (typeof BROWSER_TOOL_NAMES)[number];

/**
 * Optional postcondition knobs shared by every target-bearing tool.
 * Ported from `v2:backend/internal/planner/tools.go`.
 */
const expectProperties: Record<string, z.ZodType> = {
  expect_text: z
    .string()
    .describe("text that must become visible after the action")
    .optional(),
  expect_gone: z
    .string()
    .describe("text that must no longer be visible after the action")
    .optional(),
  expect_url: z
    .string()
    .describe("substring the page url must contain after the action")
    .optional(),
  expect_value: z
    .string()
    .describe("value the target element must hold after the action")
    .optional(),
  expect_element: z
    .string()
    .describe(
      "target element state: visible, hidden, enabled, disabled, checked, or unchecked",
    )
    .optional(),
  expect_attribute: z
    .string()
    .describe("target attribute assertion in attr=value form")
    .optional(),
  expect_count: z
    .string()
    .describe("expected count for the grounded target locator")
    .optional(),
};

/** Returns the shared optional expect_* knobs, omitting `excluded` keys. */
function optionalExpects(excluded: readonly string[]): Record<string, z.ZodType> {
  const result: Record<string, z.ZodType> = {};
  for (const [key, value] of Object.entries(expectProperties)) {
    if (!excluded.includes(key)) {
      result[key] = value;
    }
  }
  return result;
}

const targetObjectShape = {
  role: z.string().optional(),
  text: z.string().optional(),
  name: z.string().optional(),
  aliases: z.array(z.string()).optional(),
};

const targetScopeShape = {
  kind: z
    .string()
    .describe(
      "container kind such as card, form, dialog, table_row, list_item, frame",
    )
    .optional(),
  contains_text: z
    .string()
    .describe("text that must appear inside the scope")
    .optional(),
  ref: z
    .string()
    .describe("scope ref from the current page view, if known")
    .optional(),
};

const targetSpecSchema = z
  .object({
    object: z.object(targetObjectShape),
    scope: z.object(targetScopeShape).optional(),
    relation: z
      .string()
      .describe("relation such as within, near, label_for, row_contains")
      .optional(),
  })
  .describe(
    "structured semantic target. It may include object/scope hints, but never CSS or XPath; the system grounds it to a verified locator.",
  );

function candidateIdSchema(action: string, assertion: boolean) {
  let description = `candidate id from the latest page view only when its advertised action is exactly ${action}, matching this tool; never use a candidate advertised for another action`;
  if (assertion) {
    description +=
      "; never reuse a click or input candidate. If no compatible assertion candidate exists, use hint or target grounded in current elements";
  }
  return z.string().describe(description).optional();
}

/**
 * A tool definition consumed by the MCP server. The `inputSchema` is a zod
 * raw shape so the server can register it directly with `McpServer.tool`.
 */
export interface BrowserToolSchema {
  name: BrowserToolName;
  description: string;
  inputSchema: Record<string, z.ZodType>;
}

const ACTION_TOOLS: BrowserToolSchema[] = [
  {
    name: "click",
    description:
      "Click an element on the current page. The hint is matched against the latest observation; it must resolve to exactly one visible, enabled element. If the page view includes a suitable action candidate, pass candidate_id instead of describing a low-semantics target. You must declare at least one expectation, otherwise the step cannot be verified.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element")
        .optional(),
      candidate_id: candidateIdSchema("click", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this click is needed"),
      ...expectProperties,
    },
  },
  {
    name: "input",
    description:
      "Type a value into an element on the current page. Same matching and expectation rules as click.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the input")
        .optional(),
      candidate_id: candidateIdSchema("input", false),
      target: targetSpecSchema.optional(),
      value: z.string().describe("value to type (empty string is allowed)"),
      intent: z.string().describe("why this input is needed"),
      submit: z
        .boolean()
        .describe("press Enter after typing, to submit the form (input only)")
        .optional(),
      ...expectProperties,
    },
  },
  {
    name: "select",
    description:
      "Select an option in a native select/combobox. Defaults to verifying the selected value.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the select")
        .optional(),
      candidate_id: candidateIdSchema("select", false),
      target: targetSpecSchema.optional(),
      value: z.string().describe("option label or value"),
      intent: z.string().describe("why this select is needed"),
      ...expectProperties,
    },
  },
  {
    name: "check",
    description:
      "Check a checkbox or radio target. Defaults to verifying checked state.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the checkbox/radio")
        .optional(),
      candidate_id: candidateIdSchema("check", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this check is needed"),
      ...expectProperties,
    },
  },
  {
    name: "uncheck",
    description:
      "Uncheck a checkbox target. Defaults to verifying unchecked state.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the checkbox")
        .optional(),
      candidate_id: candidateIdSchema("uncheck", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this uncheck is needed"),
      ...expectProperties,
    },
  },
  {
    name: "scroll_into_view",
    description:
      "Scroll a target into view before a later action. Declare an expectation such as expect_element=visible.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element")
        .optional(),
      candidate_id: candidateIdSchema("scroll_into_view", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this scroll is needed"),
      ...expectProperties,
    },
  },
  {
    name: "hover",
    description:
      "Hover a target, usually to open a menu. Declare the expected menu text, url, attribute, or element state.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element")
        .optional(),
      candidate_id: candidateIdSchema("hover", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this hover is needed"),
      ...expectProperties,
    },
  },
  {
    name: "dismiss_dialog",
    description:
      "Dismiss a visible dialog/modal by clicking a close control inside it. Declare expect_element=hidden when appropriate.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the dialog")
        .optional(),
      candidate_id: candidateIdSchema("dismiss_dialog", false),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("why this dialog should be dismissed"),
      ...expectProperties,
    },
  },
  {
    name: "upload_file",
    description:
      "Upload a local file through a file input. The value is the local file path.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the file input")
        .optional(),
      candidate_id: candidateIdSchema("upload_file", false),
      target: targetSpecSchema.optional(),
      value: z.string().describe("local file path"),
      intent: z.string().describe("why this upload is needed"),
      ...expectProperties,
    },
  },
];

const TARGET_ASSERTION_TOOLS: BrowserToolSchema[] = [
  {
    name: "assert_element",
    description:
      "Assert a target element state: visible, hidden, enabled, disabled, checked, or unchecked.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element")
        .optional(),
      candidate_id: candidateIdSchema("assert_element", true),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("what this assertion proves"),
      expect_element: z.string().describe("state to assert"),
      ...optionalExpects(["expect_element"]),
    },
  },
  {
    name: "assert_attribute",
    description: "Assert a target element attribute in attr=value form.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element")
        .optional(),
      candidate_id: candidateIdSchema("assert_attribute", true),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("what this assertion proves"),
      expect_attribute: z.string().describe("attr=value assertion"),
      ...optionalExpects(["expect_attribute"]),
    },
  },
  {
    name: "assert_count",
    description:
      "Assert the grounded target locator resolves to an expected count.",
    inputSchema: {
      hint: z
        .string()
        .describe("visible text or accessible name of the element set")
        .optional(),
      candidate_id: candidateIdSchema("assert_count", true),
      target: targetSpecSchema.optional(),
      intent: z.string().describe("what this assertion proves"),
      expect_count: z.string().describe("expected integer count"),
      ...optionalExpects(["expect_count"]),
    },
  },
];

export const BROWSER_TOOL_SCHEMAS: BrowserToolSchema[] = [
  {
    name: "open_page",
    description:
      "Navigate the authoring browser to an absolute url and observe the page. Must be the first tool you call. Every later target is resolved against the latest observation.",
    inputSchema: {
      url: z.string().describe("absolute url"),
      intent: z
        .string()
        .describe("why this page is opened, in the user's language"),
    },
  },
  ...ACTION_TOOLS,
  {
    name: "assert_text",
    description:
      "Assert that text is visible on the current page. Checked on the page as it is now, so it must already hold (or hold after the previous step).",
    inputSchema: {
      text: z.string().describe("text that must be visible"),
      intent: z.string().describe("what this assertion proves"),
    },
  },
  {
    name: "assert_url",
    description: "Assert that the current page url contains a substring.",
    inputSchema: {
      contains: z.string().describe("substring the url must contain"),
      intent: z.string().describe("what this assertion proves"),
    },
  },
  ...TARGET_ASSERTION_TOOLS,
  {
    name: "finish_case",
    description:
      "Finish the case only after inspecting the committed steps and ensuring every explicit expected outcome from the user's goal has committed proof on the relevant final state. Navigation or current visibility alone is not proof; use an assertion or an action postcondition that explicitly encodes the expected outcome. Setting an input earlier does not prove its value persisted on a later or final page. Final requested values and counts must be asserted there. The backend validates the case and runs it once in a fresh browser; only a case that passes that dry run is stored and sent to the user for approval. If the dry run fails you get the failing steps back and must fix them.",
    inputSchema: {
      name: z.string().describe("short case name"),
    },
  },
];
