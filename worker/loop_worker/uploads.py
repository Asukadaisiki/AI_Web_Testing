"""Read managed upload names without following links or reopening host paths."""

from __future__ import annotations

import os
import re
import shutil
import stat
from pathlib import Path

MAX_UPLOAD_BYTES = 16 * 1024 * 1024


class UploadRejected(ValueError):
    pass


def scoped_upload_root(scope: str) -> Path | None:
    configured = os.environ.get("LOOP_UPLOADS_DIR")
    if not configured:
        return None
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,200}", scope):
        raise UploadRejected("invalid upload scope")
    return Path(configured) / scope


def generation_uploads(environment_id: str, generation: int, *, delete=False):
    scope = f"{environment_id}_g{generation}"
    scoped = scoped_upload_root(scope)
    if scoped is None:
        return
    root = scoped.parent
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = open_directory(root)
    try:
        if delete:
            try:
                info = os.stat(scope, dir_fd=fd, follow_symlinks=False)
            except FileNotFoundError:
                return
            if stat.S_ISDIR(info.st_mode):
                shutil.rmtree(scope, dir_fd=fd)
            else:
                os.unlink(scope, dir_fd=fd)
        else:
            try:
                os.mkdir(scope, mode=0o700, dir_fd=fd)
            except FileExistsError:
                directory_fd = os.open(
                    scope, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd
                )
                os.close(directory_fd)
    finally:
        os.close(fd)


def open_directory(root: Path) -> int:
    """Pin every directory component, including the configured root."""
    if not root.is_absolute() or ".." in root.parts:
        raise UploadRejected("managed directory must be an absolute, link-free path")
    fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in root.parts[1:]:
            next_fd = os.open(
                part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd
            )
            os.close(fd)
            fd = next_fd
        return fd
    except BaseException:
        os.close(fd)
        raise


def read_upload(name: str, root: Path | None) -> dict:
    if (not isinstance(name, str) or not name or len(name) > 1024
            or "\\" in name or ":" in name or "\0" in name
            or any(part in ("", ".", "..") for part in name.split("/"))):
        raise UploadRejected("upload must be a managed relative file name")
    if root is None:
        raise UploadRejected("managed uploads are not configured")
    directory_fd = file_fd = None
    try:
        directory_fd = open_directory(root)
        parts = name.split("/")
        for part in parts[:-1]:
            next_fd = os.open(
                part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory_fd
            )
            os.close(directory_fd)
            directory_fd = next_fd
        file_fd = os.open(
            parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
            dir_fd=directory_fd,
        )
        before = os.fstat(file_fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                or before.st_size > MAX_UPLOAD_BYTES):
            raise UploadRejected("upload must be a bounded regular file without links")
        chunks = []
        size = 0
        while chunk := os.read(file_fd, min(65536, MAX_UPLOAD_BYTES + 1 - size)):
            chunks.append(chunk)
            size += len(chunk)
            if size > MAX_UPLOAD_BYTES:
                raise UploadRejected("upload exceeds size limit")
        after = os.fstat(file_fd)
        if (before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise UploadRejected("upload changed while being read")
        return {"name": parts[-1], "mimeType": "application/octet-stream",
                "buffer": b"".join(chunks)}
    except OSError as exc:
        raise UploadRejected("managed upload is unavailable or unsafe") from exc
    finally:
        if file_fd is not None:
            os.close(file_fd)
        if directory_fd is not None:
            os.close(directory_fd)
