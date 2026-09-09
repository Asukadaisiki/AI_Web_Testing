"""Compile structured locator contracts into Playwright locators."""

from __future__ import annotations

from browser_worker.contracts.browser_observation import (
    ContextPath,
    LeafLocatorSpec,
    LocatorSpec,
    RoleLocatorSpec,
    ScopedLocatorSpec,
    ValueLocatorSpec,
    validate_locator_spec,
)


def compile_locator(
    page_or_scope,
    raw: LocatorSpec | dict,
    *,
    context_path: ContextPath | dict | None = None,
):
    root = page_or_scope
    if context_path is not None:
        path = (
            context_path
            if isinstance(context_path, ContextPath)
            else ContextPath.model_validate(context_path)
        )
        for frame in path.frames:
            root = root.frame_locator(frame)
        for shadow_host in path.shadow_hosts:
            root = root.locator(shadow_host)
    spec = (
        raw
        if isinstance(raw, (RoleLocatorSpec, ValueLocatorSpec, ScopedLocatorSpec))
        else validate_locator_spec(raw)
    )
    if isinstance(spec, ScopedLocatorSpec):
        scope = _compile_leaf(root, spec.scope)
        return _compile_leaf(scope, spec.target)
    return _compile_leaf(root, spec)


def _compile_leaf(page_or_scope, spec: LeafLocatorSpec):
    if isinstance(spec, RoleLocatorSpec):
        options = (
            {"name": spec.name, "exact": spec.exact}
            if spec.name is not None
            else {}
        )
        return page_or_scope.get_by_role(spec.role, **options)

    value = spec.value
    if spec.kind == "label":
        return page_or_scope.get_by_label(value, exact=spec.exact)
    if spec.kind == "placeholder":
        return page_or_scope.get_by_placeholder(value, exact=spec.exact)
    if spec.kind == "text":
        return page_or_scope.get_by_text(value, exact=spec.exact)
    if spec.kind == "test_id":
        return page_or_scope.get_by_test_id(value)
    if spec.kind == "css":
        return page_or_scope.locator(value)
    if spec.kind == "xpath":
        expression = value if value.startswith("xpath=") else f"xpath={value}"
        return page_or_scope.locator(expression)
    raise ValueError(f"unsupported locator kind: {spec.kind}")
