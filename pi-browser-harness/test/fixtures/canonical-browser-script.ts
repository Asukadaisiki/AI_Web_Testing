import type { JsonObject, ScriptedResponse } from "../../src/harness-definition.js";

export function scriptedResponse(goal: string, priorToolResults: number): ScriptedResponse {
  if (goal.includes("DoesNotExist")) {
    if (priorToolResults === 0) return {
      tool: { name: "mcp__browser_harness__open_page", arguments: {
        url: "http://fixture-site/index.html", intent: "Open the local catalog",
      } },
    };
    if (priorToolResults === 1) return {
      tool: { name: "mcp__browser_harness__assert_text", arguments: {
        text: "DoesNotExist", intent: "Verify the missing product is visible",
      } },
    };
    return { text: "Final: DoesNotExist was not found." };
  }
  const tools: { name: string; arguments: JsonObject }[] = [
    { name: "open_page", arguments: {
      url: "http://fixture-site/index.html", intent: "Open the local catalog",
    } },
    { name: "input", arguments: {
      target: { object: { text: "Search items" } }, value: "alpha", submit: true,
      intent: "Filter the catalog to Alpha", expect_value: "alpha", expect_gone: "Beta",
    } },
    { name: "click", arguments: {
      target: { object: { text: "Alpha" } }, intent: "Open the Alpha item",
      expect_url: "detail.html", expect_gone: "Loading details",
    } },
    { name: "input", arguments: {
      target: { object: { text: "Quantity" } }, value: "2",
      intent: "Set quantity to two", expect_value: "2",
    } },
    { name: "click", arguments: {
      target: { object: { text: "Add to cart" } }, intent: "Add Alpha to the cart", expect_text: "Added!",
    } },
    { name: "click", arguments: {
      target: { object: { text: "View Cart" } }, intent: "Open the cart", expect_url: "cart.html",
    } },
    { name: "assert_text", arguments: {
      text: "Alpha x 2", intent: "Verify the final cart quantity",
    } },
    { name: "finish_case", arguments: { name: "canonical" } },
  ];
  const tool = tools[priorToolResults];
  return tool
    ? { tool: { name: `mcp__browser_harness__${tool.name}`, arguments: tool.arguments } }
    : { text: "Final: Alpha x 2 is in the cart." };
}
