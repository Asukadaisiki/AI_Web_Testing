"""Generic blocker detection and safe grounding recovery helpers."""

from __future__ import annotations

from playwright.async_api import Locator, Page
from playwright.async_api import Error as PlaywrightError
from playwright.async_api import TimeoutError as PlaywrightTimeoutError

from .contracts import (
    Blocker,
    CssLocator,
    HitTest,
    RecoveryAttempt,
    SIGNAL_BLOCKED_BY_AUTH,
    SIGNAL_BLOCKED_BY_CAPTCHA,
    SIGNAL_BLOCKED_BY_COOKIE_BANNER,
    SIGNAL_BLOCKED_BY_DIALOG,
    SIGNAL_BLOCKED_BY_INTERSTITIAL,
    SIGNAL_BLOCKED_BY_LOADING,
    SIGNAL_BLOCKED_BY_OVERLAY,
    VerifiedLocator,
)
from .locators import REF_ATTRIBUTE

BLOCKER_SIGNAL_BY_KIND: dict[str, str] = {
    "dialog": SIGNAL_BLOCKED_BY_DIALOG,
    "overlay": SIGNAL_BLOCKED_BY_OVERLAY,
    "interstitial": SIGNAL_BLOCKED_BY_INTERSTITIAL,
    "cookie_banner": SIGNAL_BLOCKED_BY_COOKIE_BANNER,
    "auth_wall": SIGNAL_BLOCKED_BY_AUTH,
    "captcha": SIGNAL_BLOCKED_BY_CAPTCHA,
    "loading": SIGNAL_BLOCKED_BY_LOADING,
}

AUTO_RECOVERABLE = {"dialog", "overlay", "interstitial", "cookie_banner", "loading"}
NON_BYPASSABLE = {"auth_wall", "captcha"}

_BLOCKER_JS = """
(args) => {
  const refAttr = args.refAttr;
  const fromPoint = args.fromPoint || null;
  const normalize = (s) => (s || '').replace(/\\s+/g, ' ').trim();
  const textOf = (el) => normalize(el.innerText !== undefined ? el.innerText : el.textContent);
  const cssEscape = (value) => (window.CSS && CSS.escape) ? CSS.escape(value) : String(value).replace(/([^a-zA-Z0-9_-])/g, '\\\\$1');
  const isVisible = (el) => {
    if (!el || el.nodeType !== 1) return false;
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false;
    if (parseFloat(style.opacity) === 0) return false;
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };
  const rectOf = (el) => {
    const rect = el.getBoundingClientRect();
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
  };
  const unique = (selector) => {
    try { return document.querySelectorAll(selector).length === 1; } catch (e) { return false; }
  };
  const cssPath = (el) => {
    if (!el || el.nodeType !== 1) return null;
    if (el.id && unique('#' + cssEscape(el.id))) return '#' + cssEscape(el.id);
    const parts = [];
    let cur = el;
    let guard = 0;
    while (cur && cur.nodeType === 1 && cur.tagName !== 'HTML' && guard++ < 64) {
      if (cur.id && unique('#' + cssEscape(cur.id))) {
        parts.unshift('#' + cssEscape(cur.id));
        break;
      }
      let sel = cur.tagName.toLowerCase();
      const parent = cur.parentElement;
      if (parent) {
        const sameTag = Array.prototype.filter.call(parent.children, (c) => c.tagName === cur.tagName);
        if (sameTag.length > 1) sel += ':nth-of-type(' + (sameTag.indexOf(cur) + 1) + ')';
      }
      parts.unshift(sel);
      cur = cur.parentElement;
    }
    const path = parts.join(' > ');
    return unique(path) ? path : null;
  };
  const blocksViewport = (el) => {
    const rect = el.getBoundingClientRect();
    const area = Math.max(0, rect.width) * Math.max(0, rect.height);
    const viewport = Math.max(1, window.innerWidth * window.innerHeight);
    return area / viewport >= 0.18;
  };
  const fixedLike = (el) => {
    const style = window.getComputedStyle(el);
    return style.position === 'fixed' || style.position === 'sticky' || el.tagName === 'DIALOG' ||
      el.getAttribute('role') === 'dialog' || el.getAttribute('aria-modal') === 'true';
  };
  const classify = (el) => {
    if (!el || el.nodeType !== 1 || !isVisible(el)) return null;
    const text = textOf(el).toLowerCase();
    const attrs = [
      el.id || '', el.className || '', el.getAttribute('role') || '',
      el.getAttribute('aria-label') || '', el.getAttribute('title') || '',
      el.getAttribute('src') || ''
    ].join(' ').toLowerCase();
    const haystack = text + ' ' + attrs;
    if (/recaptcha|g-recaptcha|hcaptcha|captcha/.test(haystack)) {
      return { kind: 'captcha', confidence: 'high', reason: 'captcha marker is visible' };
    }
    if (/(sign in required|sign in|log in|login|authenticate|password required|create account)/.test(text) && blocksViewport(el)) {
      return { kind: 'auth_wall', confidence: 'high', reason: 'visible authentication prompt covers the page' };
    }
    if (/(cookie|cookies|consent|privacy|gdpr)/.test(text) && (blocksViewport(el) || fixedLike(el))) {
      return { kind: 'cookie_banner', confidence: 'high', reason: 'visible cookie or consent prompt covers content' };
    }
    if (/(loading|please wait|spinner)/.test(haystack) && blocksViewport(el)) {
      return { kind: 'loading', confidence: 'medium', reason: 'visible loading state covers content' };
    }
    if (/google_vignette|interstitial|advertisement|sponsored/.test(haystack) && blocksViewport(el)) {
      return { kind: 'interstitial', confidence: 'medium', reason: 'visible interstitial or ad marker covers content' };
    }
    if (el.tagName === 'IFRAME' && blocksViewport(el)) {
      return { kind: 'interstitial', confidence: 'medium', reason: 'large visible iframe covers content' };
    }
    if ((el.tagName === 'DIALOG' || el.getAttribute('role') === 'dialog' || el.getAttribute('aria-modal') === 'true') && blocksViewport(el)) {
      return { kind: 'dialog', confidence: 'high', reason: 'modal dialog is visible' };
    }
    if (fixedLike(el) && blocksViewport(el)) {
      return { kind: 'overlay', confidence: 'medium', reason: 'fixed or sticky layer covers content' };
    }
    return null;
  };
  const dismissCandidates = (root, kind) => {
    if (kind === 'auth_wall' || kind === 'captcha' || kind === 'loading') return [];
    const safe = /(close|dismiss|cancel|no thanks|not now|accept|agree|reject|allow all|got it|^x$|^×$)/i;
    const out = [];
    const candidates = root.querySelectorAll('button, a, [role="button"], input[type="button"], input[type="submit"]');
    for (const el of candidates) {
      if (!isVisible(el)) continue;
      const label = normalize([
        el.innerText || el.value || '',
        el.getAttribute('aria-label') || '',
        el.getAttribute('title') || '',
        el.id || '',
        el.name || '',
        el.getAttribute('data-testid') || ''
      ].join(' '));
      if (!safe.test(label)) continue;
      const css = cssPath(el);
      if (css) out.push({ css, text: label });
    }
    return out.slice(0, 3);
  };
  const blockerFromElement = (start) => {
    let cur = start;
    let fallback = null;
    while (cur && cur.nodeType === 1 && cur !== document.documentElement) {
      const info = classify(cur);
      if (info) {
        fallback = { element: cur, ...info };
        if (info.kind !== 'overlay') break;
      }
      cur = cur.parentElement;
    }
    if (!fallback) return null;
    return {
      kind: fallback.kind,
      ref: fallback.element.getAttribute(refAttr) || fallback.element.id || cssPath(fallback.element) || null,
      confidence: fallback.confidence,
      reason: fallback.reason,
      dismiss_candidates: dismissCandidates(fallback.element, fallback.kind)
    };
  };
  if (fromPoint) {
    const hit = document.elementFromPoint(fromPoint.x, fromPoint.y);
    return { blockers: hit ? [blockerFromElement(hit)].filter(Boolean) : [] };
  }
  const roots = [];
  document.querySelectorAll('dialog,[role="dialog"],[aria-modal="true"],iframe,.g-recaptcha,[id*="captcha" i],[class*="captcha" i],body *')
    .forEach((el) => {
      if (!isVisible(el)) return;
      if (!fixedLike(el) && !/(captcha|recaptcha|hcaptcha)/i.test([el.id, el.className].join(' '))) return;
      const blocker = blockerFromElement(el);
      if (blocker) roots.push(blocker);
    });
  const seen = new Set();
  const out = [];
  for (const blocker of roots) {
    const key = blocker.kind + ':' + (blocker.ref || blocker.reason);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(blocker);
  }
  return { blockers: out.slice(0, 10) };
}
"""

_HIT_TEST_JS = """
(el, args) => {
  const refAttr = args.refAttr;
  const rect = el.getBoundingClientRect();
  const points = [
    { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 },
    { x: rect.left + Math.min(rect.width - 1, Math.max(1, rect.width * .25)), y: rect.top + Math.min(rect.height - 1, Math.max(1, rect.height * .5)) },
    { x: rect.left + Math.min(rect.width - 1, Math.max(1, rect.width * .75)), y: rect.top + Math.min(rect.height - 1, Math.max(1, rect.height * .5)) },
  ];
  const textOf = (node) => ((node && (node.innerText || node.textContent)) || '').replace(/\\s+/g, ' ').trim();
  for (const point of points) {
    if (point.x < 0 || point.y < 0 || point.x > window.innerWidth || point.y > window.innerHeight) continue;
    const hit = document.elementFromPoint(point.x, point.y);
    const covered = !!hit && hit !== el && !el.contains(hit);
    return {
      target_ref: el.getAttribute(refAttr) || el.id || null,
      x: point.x,
      y: point.y,
      hit_ref: hit ? (hit.getAttribute(refAttr) || hit.id || null) : null,
      hit_tag: hit ? hit.tagName.toLowerCase() : null,
      hit_role: hit ? hit.getAttribute('role') : null,
      hit_text: hit ? textOf(hit).slice(0, 120) : null,
      covered: covered,
    };
  }
  return {
    target_ref: el.getAttribute(refAttr) || el.id || null,
    x: rect.left + rect.width / 2,
    y: rect.top + rect.height / 2,
    hit_ref: null,
    hit_tag: null,
    hit_role: null,
    hit_text: null,
    covered: false,
  };
}
"""


def signal_for_blocker(kind: str) -> str:
    return BLOCKER_SIGNAL_BY_KIND.get(kind, SIGNAL_BLOCKED_BY_OVERLAY)


async def detect_blockers(page: Page, *, x: float | None = None, y: float | None = None) -> list[Blocker]:
    payload = await page.evaluate(
        _BLOCKER_JS,
        {
            "refAttr": REF_ATTRIBUTE,
            "fromPoint": {"x": x, "y": y} if x is not None and y is not None else None,
        },
    )
    blockers: list[Blocker] = []
    for item in (payload or {}).get("blockers", []):
        dismiss_locators: list[VerifiedLocator] = []
        for candidate in item.get("dismiss_candidates") or []:
            css = candidate.get("css")
            if not css:
                continue
            try:
                count = await page.locator(css).count()
            except Exception:
                continue
            if count == 1:
                dismiss_locators.append(CssLocator(css=css, match_count=1))
        blockers.append(
            Blocker(
                kind=str(item.get("kind") or "overlay"),
                ref=item.get("ref"),
                confidence=item.get("confidence") or "medium",
                dismiss_candidates=dismiss_locators,
                reason=str(item.get("reason") or ""),
            )
        )
    return blockers


async def target_hit_test(page: Page, locator: Locator) -> HitTest:
    await locator.scroll_into_view_if_needed(timeout=2000)
    payload = await locator.evaluate(_HIT_TEST_JS, {"refAttr": REF_ATTRIBUTE})
    hit = HitTest(**(payload or {}))
    if hit.covered:
        blockers = await detect_blockers(page, x=hit.x, y=hit.y)
        if blockers:
            hit.blocker_kind = blockers[0].kind
    return hit


async def blocker_for_hit(page: Page, hit: HitTest) -> Blocker:
    blockers = await detect_blockers(page, x=hit.x, y=hit.y)
    if blockers:
        blocker = blockers[0]
        if hit.target_ref and not blocker.covers_target_ref:
            blocker.covers_target_ref = hit.target_ref
        return blocker
    return Blocker(
        kind="overlay",
        ref=hit.hit_ref,
        confidence="low",
        covers_target_ref=hit.target_ref,
        reason="hit-test point is covered by another visible element",
    )


async def attempt_recovery(page: Page, blocker: Blocker, timeout_ms: int) -> RecoveryAttempt:
    url_before = page.url
    if blocker.kind in NON_BYPASSABLE:
        return RecoveryAttempt(
            blocker=blocker,
            action="none",
            succeeded=False,
            reason=f"{blocker.kind} requires user input",
            url_before=url_before,
            url_after=page.url,
        )
    if blocker.kind == "loading":
        try:
            await page.wait_for_timeout(min(timeout_ms, 2000))
        except Exception:
            pass
        return RecoveryAttempt(
            blocker=blocker,
            action="wait",
            succeeded=True,
            url_before=url_before,
            url_after=page.url,
        )
    if blocker.dismiss_candidates:
        locator = blocker.dismiss_candidates[0]
        if isinstance(locator, CssLocator):
            try:
                await page.locator(locator.css).click(timeout=timeout_ms)
                return RecoveryAttempt(
                    blocker=blocker,
                    action="dismiss",
                    succeeded=True,
                    url_before=url_before,
                    url_after=page.url,
                )
            except (PlaywrightTimeoutError, PlaywrightError) as exc:
                return RecoveryAttempt(
                    blocker=blocker,
                    action="dismiss",
                    succeeded=False,
                    reason=str(exc),
                    url_before=url_before,
                    url_after=page.url,
                )
    if blocker.kind == "dialog":
        try:
            await page.keyboard.press("Escape")
            return RecoveryAttempt(
                blocker=blocker,
                action="escape",
                succeeded=True,
                url_before=url_before,
                url_after=page.url,
            )
        except PlaywrightError as exc:
            return RecoveryAttempt(
                blocker=blocker,
                action="escape",
                succeeded=False,
                reason=str(exc),
                url_before=url_before,
                url_after=page.url,
            )
    return RecoveryAttempt(
        blocker=blocker,
        action="none",
        succeeded=False,
        reason="no verified safe dismiss candidate",
        url_before=url_before,
        url_after=page.url,
    )


__all__ = [
    "AUTO_RECOVERABLE",
    "NON_BYPASSABLE",
    "attempt_recovery",
    "blocker_for_hit",
    "detect_blockers",
    "signal_for_blocker",
    "target_hit_test",
]
