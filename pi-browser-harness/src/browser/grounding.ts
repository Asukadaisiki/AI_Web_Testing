import type {
  Action,
  ActionCandidate,
  Element,
  Grounding,
  Locator,
  Observation,
  TargetObject,
  TargetScope,
  TargetSpec,
} from "./contract.js";

export const GROUNDING_ERROR_CODES = [
  "target_not_found",
  "target_ambiguous",
  "target_hint_empty",
  "scope_not_found",
  "candidate_not_found",
  "candidate_incompatible",
] as const;

export type GroundingErrorCode = (typeof GROUNDING_ERROR_CODES)[number];

export interface CandidateView {
  ref: string;
  role?: string;
  name?: string;
  text?: string;
  score: number;
}

export interface TargetRequest {
  action: Action;
  hint?: string;
  spec?: TargetSpec;
  candidate_id?: string;
}

export interface GroundedTarget {
  element: Element;
  locator: Locator;
  candidate_id: string;
  grounding: Grounding;
}

export class GroundingError extends Error {
  readonly code: GroundingErrorCode;
  readonly candidates: CandidateView[];

  constructor(
    code: GroundingErrorCode,
    message: string,
    candidates: CandidateView[] = [],
  ) {
    super(message);
    this.name = "GroundingError";
    this.code = code;
    this.candidates = candidates;
  }
}

interface ScoredElement {
  element: Element;
  score: number;
}

interface ResolvedElement {
  element: Element;
  locator: Locator;
  candidates: CandidateView[];
}

const STRICT_SCOPE_CANDIDATE_SCORE_MARGIN = 20;
const STABLE_DESCRIPTIVE_ATTRIBUTES = new Set([
  "aria-label",
  "title",
  "placeholder",
  "id",
  "name",
  "data-testid",
]);

export function resolveTarget(
  request: TargetRequest,
  observation: Observation,
): GroundedTarget {
  const requestedCandidateID = request.candidate_id?.trim() ?? "";
  if (requestedCandidateID !== "") {
    const resolved = resolveActionCandidate(
      request.action,
      requestedCandidateID,
      observation,
    );
    return groundedTarget(
      resolved.element,
      resolved.locator,
      resolved.candidate.candidate_id,
      observation,
    );
  }

  const hint = request.hint ?? "";
  if (request.spec === undefined) {
    try {
      const resolved = resolveHint(hint, observation.elements);
      return groundedTarget(
        resolved.element,
        resolved.locator,
        elementCandidateID(resolved.element, resolved.locator),
        observation,
      );
    } catch (error) {
      if (!(error instanceof GroundingError)) {
        throw error;
      }
      const fallback = resolveSemanticActionCandidate(
        request.action,
        hint,
        undefined,
        observation,
        error.candidates,
      );
      if (fallback !== undefined) {
        return groundedTarget(
          fallback.element,
          fallback.locator,
          fallback.candidate.candidate_id,
          observation,
        );
      }
      throw error;
    }
  }

  let spec = request.spec;
  if (objectHints(normalizeTargetObject(spec)).length === 0 && normalize(hint) !== "") {
    spec = {
      ...spec,
      object: { ...spec.object, text: hint },
    };
  }
  try {
    const resolved = resolveSpec(spec, observation);
    return groundedTarget(
      resolved.element,
      resolved.locator,
      elementCandidateID(resolved.element, resolved.locator),
      observation,
    );
  } catch (error) {
    if (!(error instanceof GroundingError)) {
      throw error;
    }
    const fallback = resolveSemanticActionCandidate(
      request.action,
      hint,
      spec,
      observation,
      error.candidates,
    );
    if (fallback !== undefined) {
      return groundedTarget(
        fallback.element,
        fallback.locator,
        fallback.candidate.candidate_id,
        observation,
      );
    }
    throw error;
  }
}

function groundedTarget(
  element: Element,
  locator: Locator,
  candidateID: string,
  observation: Observation,
): GroundedTarget {
  return {
    element,
    locator,
    candidate_id: candidateID,
    grounding: {
      observation_id: observation.observation_id,
      page_state_id: observation.page_state_id,
      candidate_id: candidateID,
      page_url: observation.url,
    },
  };
}

function resolveHint(hint: string, elements: Element[]): ResolvedElement {
  if (normalize(hint) === "") {
    throw new GroundingError(
      "target_hint_empty",
      "target hint must not be empty",
    );
  }
  const actionable = scoreElements(elements, (element) =>
    matchScore(hint, element),
  );
  return selectScoredElement(
    actionable,
    `no element matches hint ${JSON.stringify(hint)}`,
    `hint ${JSON.stringify(hint)} matches multiple elements equally well; use a more specific hint`,
  );
}

function resolveSpec(
  spec: TargetSpec,
  observation: Observation,
): ResolvedElement {
  const object = normalizeTargetObject(spec);
  const elements =
    spec.scope === undefined
      ? observation.elements
      : elementsInScope(spec.scope, observation);
  const hints = objectHints(object);
  if (hints.length === 0 && normalize(object.role ?? "") === "") {
    throw new GroundingError(
      "target_hint_empty",
      "target object must include text, name, aliases, or role",
    );
  }

  const requestedRole = normalize(object.role ?? "");
  const actionable = scoreElements(
    elements,
    (element) => matchScoreSpec(hints, element),
    (element) =>
      requestedRole === "" || normalize(element.role ?? "") === requestedRole,
  );
  return selectScoredElement(
    actionable,
    "no element matches target object",
    "target object matches multiple elements equally well; add or narrow a scope",
  );
}

function scoreElements(
  elements: Element[],
  scorer: (element: Element) => number,
  predicate: (element: Element) => boolean = () => true,
): ScoredElement[] {
  return elements
    .filter(
      (element) =>
        element.visible &&
        element.enabled &&
        element.locators.length > 0 &&
        predicate(element),
    )
    .map((element) => ({ element, score: scorer(element) }))
    .sort(
      (left, right) =>
        right.score - left.score ||
        left.element.ref.localeCompare(right.element.ref),
    );
}

function selectScoredElement(
  actionable: ScoredElement[],
  notFoundMessage: string,
  ambiguousMessage: string,
): ResolvedElement {
  if (actionable.length === 0) {
    throw new GroundingError(
      "target_not_found",
      "the current scope has no visible, enabled element with a verified locator",
    );
  }
  const best = actionable[0]!;
  if (best.score === 0) {
    throw new GroundingError(
      "target_not_found",
      notFoundMessage,
      describeCandidates(actionable, true),
    );
  }
  if (actionable[1]?.score === best.score) {
    throw new GroundingError(
      "target_ambiguous",
      ambiguousMessage,
      describeCandidates(actionable, false),
    );
  }
  return {
    element: best.element,
    locator: best.element.locators[0]!,
    candidates: [],
  };
}

function describeCandidates(
  items: ScoredElement[],
  includeZero: boolean,
): CandidateView[] {
  return items
    .filter((item) => includeZero || item.score > 0)
    .slice(0, 10)
    .map(({ element, score }) => ({
      ref: element.ref,
      ...(element.role ? { role: element.role } : {}),
      ...(element.name ? { name: element.name } : {}),
      ...(element.text ? { text: element.text } : {}),
      score,
    }));
}

function elementsInScope(
  scope: TargetScope,
  observation: Observation,
): Element[] {
  const refs = new Set(
    observation.structures
      .filter(
        (node) =>
          (scope.ref === undefined || node.ref === scope.ref) &&
          (scope.kind === undefined || node.kind === scope.kind) &&
          (scope.contains_text === undefined ||
            scopeHasText(node.ref, node.full_text, observation, scope.contains_text)),
      )
      .map((node) => node.ref),
  );
  if (refs.size === 0) {
    throw new GroundingError(
      "scope_not_found",
      `no ${scope.kind ?? ""} scope contains ${JSON.stringify(scope.contains_text ?? "")}`,
      observation.structures.slice(0, 10).map((node) => ({
        ref: node.ref,
        role: node.kind,
        text: node.full_text,
        score: 0,
      })),
    );
  }

  let changed = true;
  while (changed) {
    changed = false;
    for (const node of observation.structures) {
      if (
        node.parent_ref &&
        refs.has(node.parent_ref) &&
        !refs.has(node.ref)
      ) {
        refs.add(node.ref);
        changed = true;
      }
    }
  }
  return observation.elements.filter(
    (element) =>
      (element.container_ref !== null &&
        element.container_ref !== undefined &&
        refs.has(element.container_ref)) ||
      (element.parent_ref !== null &&
        element.parent_ref !== undefined &&
        refs.has(element.parent_ref)),
  );
}

function scopeHasText(
  scopeRef: string,
  fullText: string,
  observation: Observation,
  text: string,
): boolean {
  const needle = normalize(text);
  if (needle === "" || normalize(fullText).includes(needle)) {
    return true;
  }
  return observation.elements.some((element) => {
    if (
      element.container_ref !== scopeRef &&
      element.parent_ref !== scopeRef
    ) {
      return false;
    }
    const attributes = Object.entries(element.attributes ?? {})
      .flat()
      .join(" ");
    return [element.name, element.text, element.full_text]
      .filter((value): value is string => typeof value === "string")
      .some((value) => normalize(value).includes(needle)) ||
      normalizeAlias(attributes).includes(normalizeAlias(text));
  });
}

function resolveActionCandidate(
  action: Action,
  candidateID: string,
  observation: Observation,
): {
  element: Element;
  locator: Locator;
  candidate: ActionCandidate;
} {
  const candidate = observation.action_candidates.find(
    (item) => item.candidate_id === candidateID,
  );
  if (candidate === undefined) {
    throw new GroundingError(
      "candidate_not_found",
      `candidate ${JSON.stringify(candidateID)} is not present in the latest observation`,
    );
  }
  if (candidate.action !== action) {
    throw new GroundingError(
      "candidate_incompatible",
      `candidate ${JSON.stringify(candidateID)} supports action ${JSON.stringify(candidate.action)}, not ${JSON.stringify(action)}`,
    );
  }
  const element = observation.elements.find(
    (item) => item.ref === candidate.target_ref,
  );
  if (element === undefined || !element.visible || !element.enabled) {
    throw new GroundingError(
      "target_not_found",
      `candidate ${JSON.stringify(candidateID)} target is absent, hidden, or disabled in the latest observation`,
    );
  }
  const locator = element.locators.find((item) =>
    locatorsEqual(item, candidate.locator),
  );
  if (locator === undefined) {
    throw new GroundingError(
      "target_not_found",
      `candidate ${JSON.stringify(candidateID)} locator is not verified on its target in the latest observation`,
    );
  }
  return { element, locator, candidate };
}

function resolveSemanticActionCandidate(
  action: Action,
  hint: string,
  spec: TargetSpec | undefined,
  observation: Observation,
  resolverCandidates: CandidateView[],
):
  | {
      element: Element;
      locator: Locator;
      candidate: ActionCandidate;
    }
  | undefined {
  const { hints, role } = semanticCandidateRequest(hint, spec);
  if (hints.length === 0) {
    return undefined;
  }
  const matches = observation.action_candidates
    .filter(
      (candidate) =>
        candidate.action === action &&
        candidateRoleCompatible(role, candidate.role ?? ""),
    )
    .map((candidate) => ({
      candidate,
      score: semanticCandidateScore(hints, candidate),
    }))
    .filter((item) => item.score > 0);
  if (matches.length !== 1) {
    return undefined;
  }
  const match = matches[0]!;
  if (
    hasValidStrictScope(spec, observation) &&
    match.score <
      Math.max(0, ...resolverCandidates.map((candidate) => candidate.score)) +
        STRICT_SCOPE_CANDIDATE_SCORE_MARGIN
  ) {
    return undefined;
  }
  try {
    return resolveActionCandidate(
      action,
      match.candidate.candidate_id,
      observation,
    );
  } catch {
    return undefined;
  }
}

function semanticCandidateRequest(
  hint: string,
  spec: TargetSpec | undefined,
): { hints: string[]; role: string } {
  const values = [hint];
  let role = "";
  if (spec !== undefined) {
    const object = normalizeTargetObject(spec);
    role = object.role ?? "";
    values.push(object.name ?? "", object.text ?? "", ...(object.aliases ?? []));
  }
  return {
    hints: uniqueNormalized(values, normalizeAlias),
    role,
  };
}

function semanticCandidateScore(
  hints: string[],
  candidate: ActionCandidate,
): number {
  const values = [
    candidate.name ?? "",
    candidate.text ?? "",
    ...(candidate.aliases ?? []),
    ...Object.entries(candidate.attributes ?? {})
      .filter(([key]) => STABLE_DESCRIPTIVE_ATTRIBUTES.has(key.toLowerCase().trim()))
      .map(([, value]) => value),
  ];
  let best = 0;
  for (const hint of hints) {
    for (const value of values) {
      best = Math.max(best, semanticValueScore(hint, value));
    }
  }
  return best;
}

function semanticValueScore(hint: string, value: string): number {
  const normalizedHint = normalizeAlias(hint);
  const normalizedValue = normalizeAlias(value);
  if (normalizedHint === "" || normalizedValue === "") {
    return 0;
  }
  if (normalizedHint === normalizedValue) {
    return 100;
  }
  const hintTokens = new Set(normalizedHint.split(" "));
  const valueTokens = new Set(normalizedValue.split(" "));
  const overlap = [...hintTokens].filter((token) => valueTokens.has(token)).length;
  if (overlap < 2) {
    return 0;
  }
  if (overlap === hintTokens.size) {
    return 80;
  }
  if (overlap === valueTokens.size) {
    return 60;
  }
  return 0;
}

function hasValidStrictScope(
  spec: TargetSpec | undefined,
  observation: Observation,
): boolean {
  if (spec?.scope === undefined) {
    return false;
  }
  const scope = spec.scope;
  if (
    normalize(scope.ref ?? "") === "" &&
    normalize(scope.kind ?? "") === "" &&
    normalize(scope.contains_text ?? "") === ""
  ) {
    return false;
  }
  try {
    elementsInScope(scope, observation);
    return true;
  } catch {
    return false;
  }
}

function normalizeTargetObject(spec: TargetSpec): TargetObject {
  return {
    ...(spec.object.role ?? spec.role
      ? { role: spec.object.role ?? spec.role }
      : {}),
    ...(spec.object.text ?? spec.text
      ? { text: spec.object.text ?? spec.text }
      : {}),
    ...(spec.object.name ?? spec.name
      ? { name: spec.object.name ?? spec.name }
      : {}),
    ...((spec.object.aliases?.length ?? 0) > 0 || (spec.aliases?.length ?? 0) > 0
      ? { aliases: spec.object.aliases ?? spec.aliases }
      : {}),
  };
}

function objectHints(object: TargetObject): string[] {
  return uniqueNormalized(
    [object.name ?? "", object.text ?? "", ...(object.aliases ?? [])],
    normalize,
  );
}

function matchScoreSpec(hints: string[], element: Element): number {
  if (hints.length === 0) {
    return 50;
  }
  return Math.max(
    ...hints.flatMap((hint) => [
      matchScore(hint, element),
      attributeScore(hint, element.attributes ?? {}),
    ]),
  );
}

function matchScore(hint: string, element: Element): number {
  const needle = normalize(hint);
  const name = normalize(element.name ?? "");
  const text = normalize(element.text ?? "");
  if (name !== "" && name === needle) return 100;
  if (text !== "" && text === needle) return 95;
  if (name !== "" && name.includes(needle)) return 80;
  if (text !== "" && text.includes(needle)) return 70;
  if (name !== "" && needle.includes(name)) return 60;
  if (text !== "" && needle.includes(text)) return 50;
  return 0;
}

function attributeScore(
  hint: string,
  attributes: Record<string, string>,
): number {
  const needle = normalizeAlias(hint);
  if (needle === "") {
    return 0;
  }
  let best = 0;
  for (const [key, value] of Object.entries(attributes)) {
    const haystack = normalizeAlias(`${key} ${value}`);
    if (haystack === needle) best = Math.max(best, 90);
    else if (haystack.includes(needle) || needle.includes(haystack)) {
      best = Math.max(best, 75);
    } else if (tokenOverlap(needle, haystack) >= 2) {
      best = Math.max(best, 65);
    }
  }
  return best;
}

function tokenOverlap(left: string, right: string): number {
  const leftTokens = new Set(left.split(" "));
  return right.split(" ").filter((token) => leftTokens.has(token)).length;
}

function candidateRoleCompatible(
  requestedRole: string,
  candidateRole: string,
): boolean {
  return (
    normalizeAlias(requestedRole) === "" ||
    normalizeAlias(requestedRole) === normalizeAlias(candidateRole)
  );
}

function uniqueNormalized(
  values: string[],
  normalizer: (value: string) => string,
): string[] {
  return [...new Set(values.map(normalizer).filter((value) => value !== ""))];
}

function elementCandidateID(element: Element, locator: Locator): string {
  const index = element.locators.indexOf(locator);
  return `${element.ref}:${index}`;
}

function locatorsEqual(left: Locator, right: Locator): boolean {
  return (
    left.kind === right.kind &&
    left.role === right.role &&
    left.name === right.name &&
    left.exact === right.exact &&
    left.text === right.text &&
    left.css === right.css &&
    left.match_count === right.match_count
  );
}

function normalizeAlias(value: string): string {
  return normalize(value.replaceAll("_", " ").replaceAll("-", " ").replaceAll(".", " "));
}

function normalize(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}
