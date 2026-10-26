import type {
  ActionCandidate,
  Blocker,
  CandidateRelation,
  Element,
  HitTest,
  Locator,
  Observation,
  StructureNode,
} from "./contract.js";

const MAX_PAGE_ELEMENTS = 20;
const MAX_PAGE_ACTION_CANDIDATES = 12;
const MAX_PAGE_SCOPES = 8;
const MAX_PAGE_BLOCKERS = 5;
const MAX_PAGE_ELEMENT_TEXT = 120;
const MAX_PAGE_SUMMARY_TEXT = 160;

export interface CompactFailureStep {
  action: string;
  blocker?: Blocker;
  hit_test?: HitTest;
}

export interface CompactResult {
  ok: boolean;
  relevance_terms?: string[];
  failure?: { steps: CompactFailureStep[] };
}

export interface ElementView {
  ref: string;
  tag: string;
  role?: string;
  name?: string;
  text?: string;
  value?: string;
  container_ref?: string;
  locator_kind?: string;
}

export interface ScopeView {
  ref: string;
  kind: string;
  text?: string;
  parent_ref?: string;
}

export interface BlockerView {
  kind: string;
  ref?: string;
  confidence?: string;
  covers_target_ref?: string;
  dismiss_candidates?: number;
  reason?: string;
}

export interface ActionCandidateView {
  candidate_id: string;
  kind: string;
  action: string;
  target_ref: string;
  role?: string;
  name?: string;
  text?: string;
  aliases?: string[];
  attributes?: Record<string, string>;
  relations?: CandidateRelation[];
  confidence?: string;
}

export interface PageView {
  url: string;
  title: string;
  truncated: boolean;
  truncation_reason: string | null;
  blockers: BlockerView[];
  scopes: ScopeView[];
  action_candidates: ActionCandidateView[];
  elements: ElementView[];
}

interface Rank {
  lastMatches: number;
  kind: number;
  goalMatches: number;
  confidence: number;
  viewport: number;
  stableRef: string;
}

interface Ranking {
  goalTokens: Set<string>;
  lastTokens: Set<string>;
  elementsByRef: Map<string, Element>;
  scopesByRef: Map<string, StructureNode>;
  preferredTargetRefs: Set<string>;
  dismissLocators: Locator[];
}

export function buildPageView(
  observation: Observation,
  goal: string,
  lastResult: CompactResult | null,
): PageView {
  const ranking = createRanking(observation, goal, lastResult);

  const blockers = rankItems(
    observation.blockers,
    (blocker) => {
      const text = blockerText(blocker, ranking);
      return rank(
        ranking,
        [blocker.ref, blocker.covers_target_ref, text].join(" "),
        text,
        preferredBlocker(blocker),
        blocker.confidence,
        true,
        stableRef(blocker.ref, blocker.kind, blocker.reason),
      );
    },
  );
  const candidates = rankItems(
    observation.action_candidates,
    (candidate) => {
      const target = ranking.elementsByRef.get(candidate.target_ref);
      const text = candidateText(candidate, ranking);
      return rank(
        ranking,
        [
          candidate.candidate_id,
          candidate.target_ref,
          text,
          ...(candidate.relations ?? []).map((relation) => relation.ref),
        ].join(" "),
        text,
        preferredCandidate(candidate, ranking),
        candidate.confidence,
        target?.visible_in_viewport ?? false,
        stableRef(candidate.candidate_id, candidate.target_ref),
      );
    },
  );
  const scopes = rankItems(
    observation.structures.filter((item) => item.visible),
    (item) => {
      const text = scopeText(item, ranking);
      return rank(
        ranking,
        [item.ref, item.parent_ref, text].join(" "),
        text,
        preferredPageKind(item.kind),
        "",
        boundingBoxVisible(item),
        stableRef(item.ref, item.kind, item.full_text),
      );
    },
  );
  const elements = rankItems(
    observation.elements.filter((item) => item.visible && item.enabled),
    (item) => {
      const text = elementText(item, ranking);
      return rank(
        ranking,
        [item.ref, item.parent_ref, item.container_ref, text].join(" "),
        text,
        preferredElement(item, ranking),
        "",
        item.visible_in_viewport ?? false,
        stableRef(item.ref, item.role, item.name, item.text),
      );
    },
  );

  const omitted = {
    blockers: omittedCount(blockers.length, MAX_PAGE_BLOCKERS),
    candidates: omittedCount(candidates.length, MAX_PAGE_ACTION_CANDIDATES),
    scopes: omittedCount(scopes.length, MAX_PAGE_SCOPES),
    elements: omittedCount(elements.length, MAX_PAGE_ELEMENTS),
  };
  const compactBlockers = blockers
    .slice(0, MAX_PAGE_BLOCKERS)
    .map(toBlockerView);
  const compactCandidates = candidates
    .slice(0, MAX_PAGE_ACTION_CANDIDATES)
    .map(toCandidateView);
  const compactScopes = scopes.slice(0, MAX_PAGE_SCOPES).map(toScopeView);
  const compactElements = elements
    .slice(0, MAX_PAGE_ELEMENTS)
    .map(toElementView);

  let truncationReason = observation.truncation_reason;
  const compactOmitted =
    omitted.blockers + omitted.candidates + omitted.scopes + omitted.elements;
  if (compactOmitted > 0) {
    const reason =
      `page_view_omitted blockers=${omitted.blockers}` +
      ` action_candidates=${omitted.candidates}` +
      ` scopes=${omitted.scopes} elements=${omitted.elements}`;
    truncationReason =
      truncationReason === null ? reason : `${truncationReason}; ${reason}`;
  }

  return {
    url: observation.url,
    title: observation.title,
    truncated: observation.truncated || compactOmitted > 0,
    truncation_reason: truncationReason,
    blockers: compactBlockers,
    scopes: compactScopes,
    action_candidates: compactCandidates,
    elements: compactElements,
  };
}

function createRanking(
  observation: Observation,
  goal: string,
  lastResult: CompactResult | null,
): Ranking {
  const ranking: Ranking = {
    goalTokens: pageViewTokens(goal),
    lastTokens: lastResultTokens(lastResult),
    elementsByRef: new Map(
      observation.elements.map((element) => [element.ref, element]),
    ),
    scopesByRef: new Map(
      observation.structures.map((item) => [item.ref, item]),
    ),
    preferredTargetRefs: new Set(),
    dismissLocators: observation.blockers.flatMap(
      (blocker) => blocker.dismiss_candidates ?? [],
    ),
  };
  for (const candidate of observation.action_candidates) {
    if (preferredCandidate(candidate, ranking)) {
      ranking.preferredTargetRefs.add(candidate.target_ref);
    }
  }
  return ranking;
}

function rankItems<T>(values: T[], ranker: (value: T) => Rank): T[] {
  return values
    .map((value) => ({ value, rank: ranker(value) }))
    .sort((left, right) => compareRank(left.rank, right.rank))
    .map((item) => item.value);
}

function rank(
  ranking: Ranking,
  relationText: string,
  semanticText: string,
  preferred: boolean,
  confidence: string,
  viewport: boolean,
  itemStableRef: string,
): Rank {
  return {
    lastMatches: tokenMatches(
      ranking.lastTokens,
      pageViewTokens(relationText),
    ),
    kind: preferred ? 1 : 0,
    goalMatches: tokenMatches(
      ranking.goalTokens,
      pageViewTokens(semanticText),
    ),
    confidence: confidenceRank(confidence),
    viewport: viewport ? 1 : 0,
    stableRef: itemStableRef,
  };
}

function compareRank(left: Rank, right: Rank): number {
  return (
    right.lastMatches - left.lastMatches ||
    right.kind - left.kind ||
    right.goalMatches - left.goalMatches ||
    right.confidence - left.confidence ||
    right.viewport - left.viewport ||
    left.stableRef.localeCompare(right.stableRef)
  );
}

function preferredBlocker(blocker: Blocker): boolean {
  return (
    dialogPageKind(blocker.kind) ||
    (blocker.dismiss_candidates?.length ?? 0) > 0
  );
}

function preferredCandidate(
  candidate: ActionCandidate,
  ranking: Ranking,
): boolean {
  if (
    normalizedPageKind(candidate.kind) === "form submit candidate" ||
    dialogPageKind(candidate.kind) ||
    ranking.dismissLocators.some((locator) =>
      locatorsEqual(locator, candidate.locator),
    ) ||
    (candidate.relations ?? []).some((relation) =>
      dialogPageKind(relation.type),
    )
  ) {
    return true;
  }
  const target = ranking.elementsByRef.get(candidate.target_ref);
  return target !== undefined && scopeHasDialogKind(target.container_ref, ranking);
}

function preferredElement(element: Element, ranking: Ranking): boolean {
  return (
    ranking.preferredTargetRefs.has(element.ref) ||
    scopeHasDialogKind(element.container_ref, ranking)
  );
}

function scopeHasDialogKind(
  startRef: string | null | undefined,
  ranking: Ranking,
): boolean {
  let ref = startRef ?? "";
  const seen = new Set<string>();
  while (ref !== "" && !seen.has(ref)) {
    seen.add(ref);
    const item = ranking.scopesByRef.get(ref);
    if (item === undefined) {
      return false;
    }
    if (dialogPageKind(item.kind)) {
      return true;
    }
    ref = item.parent_ref ?? "";
  }
  return false;
}

function blockerText(blocker: Blocker, ranking: Ranking): string {
  const target =
    blocker.covers_target_ref === null ||
    blocker.covers_target_ref === undefined
      ? undefined
      : ranking.elementsByRef.get(blocker.covers_target_ref);
  return [blocker.kind, blocker.reason, target && elementText(target, ranking)]
    .filter(Boolean)
    .join(" ");
}

function candidateText(
  candidate: ActionCandidate,
  ranking: Ranking,
): string {
  const target = ranking.elementsByRef.get(candidate.target_ref);
  return [
    candidate.kind,
    candidate.role,
    candidate.name,
    candidate.text,
    ...(candidate.aliases ?? []),
    ...Object.entries(candidate.attributes ?? {}).flat(),
    ...(candidate.relations ?? []).flatMap((relation) => [
      relation.type,
      relation.label,
    ]),
    target && elementText(target, ranking),
  ]
    .filter((value): value is string => typeof value === "string")
    .join(" ");
}

function scopeText(scope: StructureNode, ranking: Ranking): string {
  const values: string[] = [];
  const seen = new Set<string>();
  let current: StructureNode | undefined = scope;
  while (current !== undefined) {
    values.push(
      current.kind,
      current.role ?? "",
      current.full_text,
      ...Object.entries(current.attributes ?? {}).flat(),
    );
    const parentRef = current.parent_ref ?? "";
    if (parentRef === "" || seen.has(parentRef)) {
      break;
    }
    seen.add(parentRef);
    current = ranking.scopesByRef.get(parentRef);
  }
  return values.join(" ");
}

function elementText(element: Element, ranking: Ranking): string {
  const values = [
    element.tag,
    element.role,
    element.name,
    element.text,
    element.own_text,
    element.full_text,
    element.value,
    ...Object.entries(element.attributes ?? {}).flat(),
  ].filter((value): value is string => typeof value === "string");
  const scope =
    element.container_ref === null || element.container_ref === undefined
      ? undefined
      : ranking.scopesByRef.get(element.container_ref);
  if (scope !== undefined) {
    values.push(scopeText(scope, ranking));
  }
  return values.join(" ");
}

function lastResultTokens(lastResult: CompactResult | null): Set<string> {
  if (lastResult === null) {
    return new Set();
  }
  const values: string[] = [];
  if (lastResult.ok) {
    values.push(...(lastResult.relevance_terms ?? []));
  }
  for (const step of lastResult.failure?.steps ?? []) {
    values.push(step.action);
    if (step.blocker !== undefined) {
      values.push(
        step.blocker.kind,
        step.blocker.ref ?? "",
        step.blocker.covers_target_ref ?? "",
      );
    }
    if (step.hit_test !== undefined) {
      values.push(
        step.hit_test.target_ref ?? "",
        step.hit_test.hit_ref ?? "",
        step.hit_test.blocker_kind ?? "",
      );
    }
  }
  const tokens = pageViewTokens(values.join(" "));
  for (const token of [
    "action",
    "click",
    "current",
    "error",
    "failed",
    "failure",
    "input",
    "on",
    "page",
    "recorded",
    "step",
    "target",
  ]) {
    tokens.delete(token);
  }
  return tokens;
}

function pageViewTokens(value: string): Set<string> {
  const tokens = new Set<string>();
  let current: string[] = [];
  let currentIsCJK = false;
  const flush = (): void => {
    if (current.length === 0) {
      return;
    }
    tokens.add(current.join(""));
    if (currentIsCJK) {
      for (let index = 0; index + 1 < current.length; index += 1) {
        tokens.add(current.slice(index, index + 2).join(""));
      }
    }
    current = [];
  };
  for (const character of value.toLowerCase()) {
    const isCJK = cjkCharacter(character);
    if (isCJK || /[\p{L}\p{N}]/u.test(character)) {
      if (current.length > 0 && currentIsCJK !== isCJK) {
        flush();
      }
      currentIsCJK = isCJK;
      current.push(character);
    } else {
      flush();
    }
  }
  flush();
  return tokens;
}

function cjkCharacter(character: string): boolean {
  return /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u.test(
    character,
  );
}

function tokenMatches(wanted: Set<string>, available: Set<string>): number {
  let matches = 0;
  for (const token of wanted) {
    if (available.has(token)) {
      matches += 1;
    }
  }
  return matches;
}

function toBlockerView(blocker: Blocker): BlockerView {
  return {
    kind: blocker.kind,
    ...(blocker.ref ? { ref: blocker.ref } : {}),
    ...(blocker.confidence ? { confidence: blocker.confidence } : {}),
    ...(blocker.covers_target_ref
      ? { covers_target_ref: blocker.covers_target_ref }
      : {}),
    ...((blocker.dismiss_candidates?.length ?? 0) > 0
      ? { dismiss_candidates: blocker.dismiss_candidates!.length }
      : {}),
    ...(blocker.reason
      ? { reason: compactText(blocker.reason, MAX_PAGE_SUMMARY_TEXT) }
      : {}),
  };
}

function toCandidateView(candidate: ActionCandidate): ActionCandidateView {
  return {
    candidate_id: candidate.candidate_id,
    kind: candidate.kind,
    action: candidate.action,
    target_ref: candidate.target_ref,
    ...(candidate.role ? { role: candidate.role } : {}),
    ...(candidate.name ? { name: candidate.name } : {}),
    ...(candidate.text
      ? { text: compactText(candidate.text, MAX_PAGE_ELEMENT_TEXT) }
      : {}),
    ...((candidate.aliases?.length ?? 0) > 0
      ? { aliases: compactStrings(candidate.aliases!, 12, 80) }
      : {}),
    ...(candidate.attributes !== undefined
      ? { attributes: compactAttributes(candidate.attributes) }
      : {}),
    ...((candidate.relations?.length ?? 0) > 0
      ? { relations: candidate.relations }
      : {}),
    ...(candidate.confidence ? { confidence: candidate.confidence } : {}),
  };
}

function toScopeView(item: StructureNode): ScopeView {
  return {
    ref: item.ref,
    kind: item.kind,
    ...(item.full_text
      ? { text: compactText(item.full_text, MAX_PAGE_SUMMARY_TEXT) }
      : {}),
    ...(item.parent_ref ? { parent_ref: item.parent_ref } : {}),
  };
}

function toElementView(element: Element): ElementView {
  return {
    ref: element.ref,
    tag: element.tag,
    ...(element.role ? { role: element.role } : {}),
    ...(element.name ? { name: element.name } : {}),
    ...(element.text
      ? { text: compactText(element.text, MAX_PAGE_ELEMENT_TEXT) }
      : {}),
    ...(element.value !== null ? { value: element.value } : {}),
    ...(element.container_ref ? { container_ref: element.container_ref } : {}),
    ...(element.locators[0] !== undefined
      ? { locator_kind: element.locators[0].kind }
      : {}),
  };
}

function compactStrings(
  values: string[],
  limit: number,
  textLimit: number,
): string[] {
  const output: string[] = [];
  for (const raw of values) {
    const value = compactText(raw, textLimit);
    if (value !== "" && !output.includes(value)) {
      output.push(value);
    }
    if (output.length === limit) {
      break;
    }
  }
  return output;
}

function compactAttributes(
  attributes: Record<string, string>,
): Record<string, string> {
  const allowed = new Set([
    "id",
    "name",
    "type",
    "title",
    "placeholder",
    "aria-label",
    "data-testid",
  ]);
  return Object.fromEntries(
    Object.entries(attributes)
      .filter(([key]) => allowed.has(key))
      .slice(0, 8)
      .map(([key, value]) => [key, compactText(value, 80)]),
  );
}

function compactText(value: string, limit: number): string {
  const normalized = value.trim().split(/\s+/u).filter(Boolean).join(" ");
  const characters = Array.from(normalized);
  if (characters.length <= limit) {
    return normalized;
  }
  if (limit <= 3) {
    return characters.slice(0, limit).join("");
  }
  return `${characters.slice(0, limit - 3).join("")}...`;
}

function confidenceRank(confidence: string): number {
  return { high: 3, medium: 2, low: 1 }[normalize(confidence)] ?? 0;
}

function preferredPageKind(kind: string): boolean {
  const normalized = normalizedPageKind(kind);
  return normalized === "form" || dialogPageKind(normalized);
}

function dialogPageKind(kind: string): boolean {
  const normalized = normalizedPageKind(kind);
  return ["dialog", "modal", "dismiss"].some((word) =>
    normalized.includes(word),
  );
}

function normalizedPageKind(kind: string): string {
  return normalize(kind.replaceAll("_", " ").replaceAll("-", " "));
}

function boundingBoxVisible(item: StructureNode): boolean {
  const box = item.bbox;
  return (
    box !== undefined &&
    box.width > 0 &&
    box.height > 0 &&
    box.x + box.width > 0 &&
    box.y + box.height > 0
  );
}

function stableRef(...values: Array<string | null | undefined>): string {
  return values.find((value) => value?.trim())?.trim() ?? "";
}

function omittedCount(count: number, limit: number): number {
  return Math.max(0, count - limit);
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

function normalize(value: string): string {
  return value.toLowerCase().trim().split(/\s+/u).filter(Boolean).join(" ");
}
