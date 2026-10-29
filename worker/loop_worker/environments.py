"""Local encrypted Browser Environment provider.

PostgreSQL/Control API owns leases. This provider enforces supplied monotonic
tokens locally, including across process restarts, and never issues control URLs.
"""

from __future__ import annotations

import asyncio
import copy
import fcntl
import json
import os
import re
import secrets
import stat
from contextlib import asynccontextmanager
from pathlib import Path
from urllib.parse import urlsplit

from cryptography.fernet import Fernet, InvalidToken
from pydantic import BaseModel, ConfigDict, Field, model_validator

from .contracts import DEFAULT_STEP_TIMEOUT_MS
from .uploads import UploadRejected, generation_uploads, open_directory


class ProviderError(Exception):
    def __init__(self, status: int, code: str, detail: str):
        super().__init__(detail)
        self.status, self.code, self.detail = status, code, detail


def conflict(detail: str) -> ProviderError:
    return ProviderError(409, "environment_conflict", detail)


class EnvironmentPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    allowed_domains: list[str] = Field(default_factory=list, max_length=100)

    @model_validator(mode="after")
    def valid_domains(self):
        for host in self.allowed_domains:
            if (len(host) > 253 or not re.fullmatch(r"[a-z0-9]+(?:[.-][a-z0-9]+)*", host)
                    or ".." in host):
                raise ValueError("allowed_domains must contain lowercase exact hostnames")
        return self

    def allows(self, url: str) -> bool:
        try:
            parts = urlsplit(url)
            port = parts.port
            return (parts.scheme in ("http", "https") and parts.hostname in self.allowed_domains
                    and parts.username is None and parts.password is None
                    and (port is None or 0 < port <= 65535))
        except ValueError:
            return False


class IdentityRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    profile_generation: int = Field(gt=0)
    fencing_token: int = Field(gt=0)


class ProvisionRequest(IdentityRequest):
    project_id: str = Field(pattern=r"^[A-Za-z0-9_-]{1,160}$")
    policy: EnvironmentPolicy = Field(default_factory=EnvironmentPolicy)


class ControlInput(IdentityRequest):
    action: str
    x: int | None = None
    y: int | None = None
    key: str | None = None
    text: str | None = None
    url: str | None = None

    @model_validator(mode="after")
    def valid_action(self):
        required = {
            "click": {"x", "y"}, "key": {"key"}, "type": {"text"}, "navigate": {"url"},
        }.get(self.action)
        supplied = {key for key in ("x", "y", "key", "text", "url")
                    if getattr(self, key) is not None}
        if required is None or supplied != required:
            raise ValueError("input accepts only click(x,y), key(key), type(text), navigate(url)")
        if self.action == "key" and self.key not in {
            "Enter", "Tab", "Shift+Tab", "Escape", "Backspace", "Delete",
            "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End",
        }:
            raise ValueError("key is not allowed")
        if self.action == "type" and (
            len(self.text) > 4096 or any(ord(c) < 32 for c in self.text)
        ):
            raise ValueError("text must be bounded and contain no control characters")
        return self


class EnvironmentProvider:
    def __init__(self):
        root = os.environ.get("LOOP_ENVIRONMENTS_DIR")
        key = os.environ.get("LOOP_ENVIRONMENT_ENCRYPTION_KEY")
        self.root = Path(root) if root else None
        self.cipher = None
        self._locks: dict[str, asyncio.Lock] = {}
        self._active: dict[str, object] = {}
        self._root_fd = None
        self._config_error = None
        if key:
            try:
                self.cipher = Fernet(key.encode("ascii"))
            except (ValueError, UnicodeError):
                self._config_error = "invalid Environment encryption key"

    def ready(self):
        if self.root is None or self.cipher is None or self._config_error:
            raise ProviderError(503, "environment_unavailable",
                                self._config_error or "Environment storage and key are required")
        if self._root_fd is None:
            try:
                self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
                self._root_fd = open_directory(self.root)
                os.fchmod(self._root_fd, 0o700)
            except (OSError, UploadRejected) as exc:
                raise ProviderError(503, "environment_unavailable",
                                    "Environment directory is unsafe or unavailable") from exc

    def validate_id(self, environment_id):
        if not re.fullmatch(r"[A-Za-z0-9_-]{1,160}", environment_id):
            raise ProviderError(400, "invalid_request", "invalid environment_id")

    def lock(self, environment_id):
        self.validate_id(environment_id)
        return self._locks.setdefault(environment_id, asyncio.Lock())

    def claim_file_lock(self, environment_id):
        self.ready()
        fd = os.open(environment_id + ".lock",
                     os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK,
                     0o600, dir_fd=self._root_fd)
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise conflict("unsafe Environment lock")
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return fd
        except BaseException as exc:
            os.close(fd)
            if isinstance(exc, BlockingIOError):
                raise conflict("Environment is exclusively occupied by another provider") from exc
            raise

    def read(self, environment_id):
        self.ready()
        try:
            fd = os.open(environment_id + ".profile",
                         os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=self._root_fd)
        except FileNotFoundError:
            return None
        try:
            info = os.fstat(fd)
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size > 32 * 1024 * 1024:
                raise ValueError("unsafe profile")
            with os.fdopen(fd, "rb") as handle:
                fd = None
                record = json.loads(self.cipher.decrypt(handle.read()))
            if record["environment_id"] != environment_id:
                raise ValueError("profile identity mismatch")
            return record
        except (InvalidToken, ValueError, KeyError) as exc:
            raise ProviderError(409, "environment_broken",
                                "encrypted profile cannot be verified; manual recovery required") from exc
        finally:
            if fd is not None:
                os.close(fd)

    def write(self, record):
        encrypted = self.cipher.encrypt(json.dumps(record, separators=(",", ":")).encode())
        if len(encrypted) > 32 * 1024 * 1024:
            raise ProviderError(409, "environment_broken", "profile exceeds storage limit")
        name = "." + secrets.token_hex(16) + ".tmp"
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                     0o600, dir_fd=self._root_fd)
        try:
            with os.fdopen(fd, "wb") as handle:
                handle.write(encrypted)
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(name, record["environment_id"] + ".profile",
                       src_dir_fd=self._root_fd, dst_dir_fd=self._root_fd)
            os.fsync(self._root_fd)
        finally:
            try:
                os.unlink(name, dir_fd=self._root_fd)
            except FileNotFoundError:
                pass

    def require_record(self, environment_id):
        record = self.read(environment_id)
        if record is None or record["state"] == "deleted":
            raise ProviderError(404, "environment_not_found", "Environment does not exist")
        return record

    def metadata(self, record):
        return {key: record[key] for key in (
            "environment_id", "profile_generation", "fencing_token", "state", "policy",
        )}

    async def describe(self, environment_id):
        async with self.lock(environment_id):
            return self.metadata(self.require_record(environment_id))

    async def provision(self, environment_id, request: ProvisionRequest):
        async with self.lock(environment_id):
            if environment_id in self._active:
                raise conflict("Environment is occupied")
            fd = self.claim_file_lock(environment_id)
            try:
                record = self.read(environment_id)
                if record is not None:
                    if (record["state"] == "ready"
                            and record["profile_generation"] == request.profile_generation
                            and record["fencing_token"] == request.fencing_token
                            and record.get("project_id") == request.project_id
                            and record["policy"] == request.policy.model_dump()):
                        generation_uploads(environment_id, request.profile_generation)
                        return self.metadata(record)
                    raise conflict("Environment already exists or was deleted")
                record = {
                    "environment_id": environment_id,
                    **request.model_dump(), "state": "ready",
                    "storage_state": {"cookies": [], "origins": []},
                }
                generation_uploads(environment_id, request.profile_generation)
                self.write(record)
                return self.metadata(record)
            finally:
                os.close(fd)

    async def reset(self, environment_id, request: IdentityRequest):
        async with self.lock(environment_id):
            session = self._active.get(environment_id)
            fd = session.environment_lock_fd if session else self.claim_file_lock(environment_id)
            try:
                record = self.require_record(environment_id)
                if (record["profile_generation"] == request.profile_generation
                        and record["fencing_token"] == request.fencing_token
                        and record["state"] == "ready"):
                    if session:
                        await session.context.close()
                        session.closed = True
                        self._active.pop(environment_id)
                        session.environment_lock_fd = None
                    generation_uploads(environment_id, request.profile_generation - 1, delete=True)
                    generation_uploads(environment_id, request.profile_generation)
                    return self.metadata(record)
                if (request.profile_generation != record["profile_generation"] + 1
                        or request.fencing_token <= record["fencing_token"]):
                    raise conflict("reset requires next generation and a newer fencing token")
                old_generation = record["profile_generation"]
                record.update(request.model_dump(), state="ready", lease_session_id=None,
                              storage_state={"cookies": [], "origins": []})
                # Fence durably before disposing the old context. Never save its state.
                self.write(record)
                if session:
                    await session.context.close()
                    session.closed = True
                    self._active.pop(environment_id)
                    session.environment_lock_fd = None
                generation_uploads(environment_id, old_generation, delete=True)
                generation_uploads(environment_id, request.profile_generation)
                return self.metadata(record)
            finally:
                if not session or session.closed:
                    os.close(fd)

    async def delete(self, environment_id, request: IdentityRequest):
        async with self.lock(environment_id):
            if environment_id in self._active:
                raise conflict("release the Environment before deletion")
            fd = self.claim_file_lock(environment_id)
            try:
                record = self.read(environment_id)
                if record is None:
                    raise ProviderError(404, "environment_not_found", "Environment does not exist")
                if (record["state"] == "deleted"
                        and record["profile_generation"] == request.profile_generation
                        and record["fencing_token"] == request.fencing_token):
                    generation_uploads(environment_id, request.profile_generation, delete=True)
                    return self.metadata(record)
                if (record["profile_generation"] != request.profile_generation
                        or request.fencing_token <= record["fencing_token"]):
                    raise conflict("delete requires current generation and a newer fencing token")
                record.update(request.model_dump(), state="deleted",
                              storage_state={"cookies": [], "origins": []})
                self.write(record)
                generation_uploads(environment_id, request.profile_generation, delete=True)
                return self.metadata(record)
            finally:
                os.close(fd)

    async def create(self, manager, request):
        environment_id = request.environment_id
        async with self.lock(environment_id):
            if environment_id in self._active:
                raise conflict("Environment already has an exclusive context")
            fd = self.claim_file_lock(environment_id)
            context = None
            try:
                record = self.require_record(environment_id)
                clean_reopen = (
                    record["state"] == "ready"
                    and record.get("lease_session_id") == request.session_id
                    and record["fencing_token"] == request.fencing_token
                )
                if (request.project_id != record.get("project_id")
                        or request.profile_generation != record["profile_generation"]
                        or request.fencing_token < record["fencing_token"]
                        or (request.fencing_token == record["fencing_token"] and not clean_reopen)):
                    raise conflict("mismatched project or stale profile generation or fencing token")
                # A repair retains the lease fence but gets a fresh browser handle.
                # Only a durably acknowledged close permits reopening that lease.
                initial_state = copy.deepcopy(
                    record.get("lease_initial_storage_state", record["storage_state"])
                    if clean_reopen else record["storage_state"]
                )
                record.update(
                    fencing_token=request.fencing_token, state="leased",
                    lease_session_id=request.session_id,
                    lease_initial_storage_state=initial_state,
                )
                self.write(record)
                browser = await manager.browser()
                context = await browser.new_context(
                    viewport={"width": 1280, "height": 800},
                    storage_state=record["storage_state"], accept_downloads=False,
                    service_workers="block",
                )
                policy = EnvironmentPolicy.model_validate(record["policy"])
                await self.install_policy(context, policy)
                page = await context.new_page()
                session = manager.register(request.session_id, context, page)
                session.project_id = request.project_id
                session.environment_id = environment_id
                session.profile_generation = request.profile_generation
                session.fencing_token = request.fencing_token
                session.environment_lock_fd = fd
                session.environment_policy = policy
                session.initial_storage_state = copy.deepcopy(initial_state)
                self._active[environment_id] = session
                return session
            except BaseException:
                if context:
                    await context.close()
                os.close(fd)
                raise

    @staticmethod
    async def install_policy(context, policy):
        async def route_request(route):
            if not policy.allows(route.request.url):
                await route.abort("blockedbyclient")
                return
            # Chromium redirect chains bypass Playwright routing, even after a
            # synthetic 302. Deny redirects rather than bypass the domain policy.
            response = await route.fetch(max_redirects=0, timeout=DEFAULT_STEP_TIMEOUT_MS)
            try:
                if response.status in (301, 302, 303, 307, 308):
                    await route.abort("blockedbyclient")
                else:
                    await route.fulfill(response=response)
            finally:
                await response.dispose()

        async def reject_socket(socket):
            await socket.close()

        await context.route("**/*", route_request)
        await context.route_web_socket("**/*", reject_socket)

    @asynccontextmanager
    async def guard(self, session, generation, token, environment_id=None, project_id=None):
        async with self.lock(session.environment_id):
            record = self.require_record(session.environment_id)
            if (session.closed or self._active.get(session.environment_id) is not session
                    or (environment_id is not None and environment_id != session.environment_id)
                    or project_id != session.project_id
                    or record.get("project_id") != session.project_id
                    or record.get("lease_session_id") != session.session_id
                    or generation != session.profile_generation or token != session.fencing_token
                    or generation != record["profile_generation"] or token != record["fencing_token"]):
                raise conflict("stale or mismatched Environment session identity")
            yield session

    @asynccontextmanager
    async def execution_guard(self, request):
        session = self._active.get(request.environment_id)
        if (session is None or session.session_id != request.session_id
                or (request.browser_session_id is not None
                    and request.browser_session_id != session.browser_session_id)
                or (session.project_id is not None and request.browser_session_id is None)):
            raise conflict("execution requires the active Environment lease and domain session")
        async with self.guard(
            session, request.profile_generation, request.fencing_token, request.environment_id,
            request.project_id,
        ):
            yield session

    def owns_domain_session(self, session_id):
        return any(session.session_id == session_id for session in self._active.values())

    async def close(self, session):
        record = self.require_record(session.environment_id)
        self.check_current_session(session, record)
        state = await session.context.storage_state(indexed_db=True)
        await session.context.close()
        record = self.require_record(session.environment_id)
        self.check_current_session(session, record)
        session.closed = True
        try:
            record.update(storage_state=state, state="ready")
            self.write(record)
        finally:
            self._active.pop(session.environment_id, None)
            os.close(session.environment_lock_fd)
            session.environment_lock_fd = None

    def check_current_session(self, session, record):
        if (session.closed or self._active.get(session.environment_id) is not session
                or record.get("project_id") != session.project_id
                or record.get("lease_session_id") != session.session_id
                or record["profile_generation"] != session.profile_generation
                or record["fencing_token"] != session.fencing_token):
            raise conflict("stale Environment session cannot persist state")

    async def shutdown(self):
        for session in list(self._active.values()):
            async with self.lock(session.environment_id):
                try:
                    await session.context.close()
                finally:
                    session.closed = True
                    os.close(session.environment_lock_fd)
                    session.environment_lock_fd = None
                    self._active.pop(session.environment_id, None)
        if self._root_fd is not None:
            os.close(self._root_fd)
            self._root_fd = None

    def check_navigation(self, session, url):
        if not session.environment_policy.allows(url):
            raise ProviderError(400, "domain_rejected", "URL is outside the Environment domain policy")

    async def input(self, session, request: ControlInput):
        page = session.page
        if request.action == "click":
            viewport = page.viewport_size
            if not (0 <= request.x < viewport["width"] and 0 <= request.y < viewport["height"]):
                raise ProviderError(400, "invalid_request", "click is outside the viewport")
            await page.mouse.click(request.x, request.y, button="left")
        elif request.action == "key":
            await page.keyboard.press(request.key)
        elif request.action == "type":
            await page.keyboard.insert_text(request.text)
        else:
            self.check_navigation(session, request.url)
            await page.goto(request.url, timeout=DEFAULT_STEP_TIMEOUT_MS, wait_until="domcontentloaded")
