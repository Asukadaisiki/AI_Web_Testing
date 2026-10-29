"""Upload security tests, without launching Chromium."""

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, patch

from loop_worker import actions


class UploadSafetyTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name).resolve()
        self.env = patch.dict(os.environ, {"LOOP_UPLOADS_DIR": str(self.root)})
        self.env.start()
        self.locator = AsyncMock()
        self.actionable = patch.object(actions, "ensure_actionable", AsyncMock(return_value=[]))
        self.stable = patch.object(actions, "wait_for_stable", AsyncMock())
        self.actionable.start()
        self.stable.start()

    async def asyncTearDown(self):
        self.stable.stop()
        self.actionable.stop()
        self.env.stop()
        self.directory.cleanup()

    async def reject(self, name):
        with self.assertRaises(actions.ActionFailure) as raised:
            await actions.upload_file_target(object(), self.locator, name, 100)
        self.assertEqual(raised.exception.kind, "upload_rejected")
        self.locator.set_input_files.assert_not_awaited()

    async def test_rejects_absolute_traversal_and_non_names(self):
        for name in (str(self.root / "secret"), "../secret", "a/../secret",
                     "/etc/passwd", "C:\\secret", "a\\secret", "", ".", "a//b",
                     {"name": "secret", "buffer": b"unmanaged"}, ["secret"]):
            with self.subTest(name=name):
                await self.reject(name)

    async def test_rejects_symlink_file_and_directory(self):
        (self.root / "real").write_bytes(b"secret")
        (self.root / "link").symlink_to(self.root / "real")
        (self.root / "alias").symlink_to(self.root, target_is_directory=True)
        await self.reject("link")
        await self.reject("alias/real")

    async def test_rejects_fifo_directory_missing_and_hardlink(self):
        os.mkfifo(self.root / "pipe")
        (self.root / "dir").mkdir()
        (self.root / "real").write_bytes(b"secret")
        os.link(self.root / "real", self.root / "hard")
        for name in ("pipe", "dir", "missing", "hard"):
            await self.reject(name)

    async def test_disabled_without_explicit_managed_root(self):
        with patch.dict(os.environ, {"LOOP_UPLOADS_DIR": ""}):
            await self.reject("file.txt")

    async def test_reads_only_managed_bytes_and_passes_no_path_to_playwright(self):
        (self.root / "nested").mkdir()
        (self.root / "nested/file.txt").write_bytes(b"managed content")
        await actions.upload_file_target(object(), self.locator, "nested/file.txt", 100)
        payload = self.locator.set_input_files.call_args.args[0]
        self.assertEqual(payload, {
            "name": "file.txt", "mimeType": "application/octet-stream",
            "buffer": b"managed content",
        })

    async def test_rejects_oversize_file(self):
        with (self.root / "large").open("wb") as handle:
            handle.truncate(16 * 1024 * 1024 + 1)
        await self.reject("large")
