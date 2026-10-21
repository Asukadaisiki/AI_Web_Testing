"""Bounded live-process suppression for duplicate operation callbacks."""

from __future__ import annotations

import asyncio
import hashlib
import json
from collections import OrderedDict
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any, Generic, TypeVar

T = TypeVar("T")


class IdempotencyConflict(Exception):
    """One operation ID was reused with a different request body."""

    def __init__(self, operation_id: str) -> None:
        super().__init__(
            f"operation {operation_id!r} was already used with a different request body"
        )
        self.operation_id = operation_id


@dataclass(frozen=True)
class _Completed(Generic[T]):
    request_hash: str
    value: T


@dataclass(frozen=True)
class _InFlight(Generic[T]):
    request_hash: str
    task: asyncio.Task[T]


class IdempotencyCache(Generic[T]):
    """Shares in-flight work and retains a bounded LRU of completed results."""

    def __init__(self, max_entries: int = 256) -> None:
        if max_entries < 1:
            raise ValueError("max_entries must be at least 1")
        self._max_entries = max_entries
        self._completed: OrderedDict[str, _Completed[T]] = OrderedDict()
        self._in_flight: dict[str, _InFlight[T]] = {}
        self._lock = asyncio.Lock()

    async def execute(
        self,
        operation_id: str,
        request_hash: str,
        factory: Callable[[], Awaitable[T]],
    ) -> T:
        if not operation_id:
            raise ValueError("operation_id is required")
        if not request_hash:
            raise ValueError("request_hash is required")

        async with self._lock:
            completed = self._completed.get(operation_id)
            if completed is not None:
                if completed.request_hash != request_hash:
                    raise IdempotencyConflict(operation_id)
                self._completed.move_to_end(operation_id)
                return completed.value

            in_flight = self._in_flight.get(operation_id)
            if in_flight is not None:
                if in_flight.request_hash != request_hash:
                    raise IdempotencyConflict(operation_id)
                task = in_flight.task
            else:
                task = asyncio.create_task(
                    self._execute_and_store(operation_id, request_hash, factory)
                )
                self._in_flight[operation_id] = _InFlight(request_hash, task)

        return await asyncio.shield(task)

    async def _execute_and_store(
        self,
        operation_id: str,
        request_hash: str,
        factory: Callable[[], Awaitable[T]],
    ) -> T:
        try:
            value = await factory()
        except BaseException:
            async with self._lock:
                self._remove_current_task(operation_id)
            raise

        async with self._lock:
            self._remove_current_task(operation_id)
            self._completed[operation_id] = _Completed(request_hash, value)
            self._completed.move_to_end(operation_id)
            while len(self._completed) > self._max_entries:
                self._completed.popitem(last=False)
        return value

    def _remove_current_task(self, operation_id: str) -> None:
        current = self._in_flight.get(operation_id)
        if current is not None and current.task is asyncio.current_task():
            self._in_flight.pop(operation_id, None)


def canonical_request_hash(value: Any) -> str:
    """Hashes the canonical JSON body, independent of object key ordering."""
    if hasattr(value, "model_dump"):
        value = value.model_dump(mode="json")
    encoded = json.dumps(
        value,
        ensure_ascii=True,
        allow_nan=False,
        separators=(",", ":"),
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "IdempotencyCache",
    "IdempotencyConflict",
    "canonical_request_hash",
]
