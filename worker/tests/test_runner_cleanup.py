"""Fresh execution contexts must be disposed even when setup fails."""

import os
import tempfile
import unittest
from unittest.mock import patch

from loop_worker.environments import EnvironmentPolicy
from loop_worker.runner import run_case
from tests.case_builder import build_case, goto_step
from tests.local_site import LocalSite
from tests.pw_support import launched_browser


class RunnerCleanupTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.config = patch.dict(os.environ, {"LOOP_ARTIFACTS_DIR": self.directory.name})
        self.config.start()

    async def asyncTearDown(self):
        self.config.stop()
        self.directory.cleanup()

    async def test_close_failure_changes_passed_execution_to_uncertain_error(self):
        with LocalSite() as site:
            async with launched_browser() as browser:
                context = await browser.new_context()
                async def failing_close():
                    raise RuntimeError("context close failed")
                try:
                    with patch.object(browser, "new_context", return_value=context), patch.object(
                        context, "close", failing_close
                    ):
                        result = await run_case(
                            build_case([goto_step(0, site.url("index.html"), "/index.html")]),
                            browser=browser, session_id="sess_cleanup",
                        )
                    self.assertEqual(result.status, "error", result.model_dump())
                    self.assertEqual(result.error.kind, "worker_error")
                    self.assertIn("cleanup_uncertain", result.error.message)
                    self.assertEqual(result.steps[0].status, "passed")
                    self.assertTrue(result.steps[0].evidence.screenshot_path)
                finally:
                    await context.close()

    async def test_setup_failures_close_the_context_and_return_executor_error(self):
        async with launched_browser() as browser:
            for phase in ("policy", "filter", "page", "timeouts", "collector"):
                with self.subTest(phase=phase):
                    context = await browser.new_context()
                    async def fail(*args, **kwargs):
                        raise RuntimeError(f"{phase} setup failed")
                    options = {}
                    if phase == "policy":
                        failure = patch("loop_worker.runner.EnvironmentProvider.install_policy", fail)
                        options["environment_policy"] = EnvironmentPolicy()
                    elif phase == "filter":
                        failure = patch("loop_worker.runner.install_resource_filter", fail)
                    elif phase == "page":
                        failure = patch.object(context, "new_page", fail)
                    elif phase == "timeouts":
                        page = await context.new_page()
                        original = context.new_page
                        def fail_timeout(*args):
                            raise RuntimeError("timeouts setup failed")
                        failure = patch.object(page, "set_default_timeout", fail_timeout)
                        async def existing_page():
                            return page
                        context.new_page = existing_page
                    else:
                        failure = patch("loop_worker.runner.EvidenceCollector", side_effect=RuntimeError(
                            "collector setup failed"
                        ))
                    try:
                        with patch.object(browser, "new_context", return_value=context), failure:
                            try:
                                result = await run_case(
                                    build_case([goto_step(0, "https://example.test/", "example.test")]),
                                    browser=browser, session_id="sess_cleanup", **options,
                                )
                            except Exception as exc:
                                self.fail(f"setup escaped cleanup handling: {exc}")
                        self.assertNotIn(context, browser.contexts, "setup leaked a context")
                        self.assertEqual(result.status, "error")
                        self.assertEqual(result.error.kind, "worker_error")
                        self.assertIn(f"{phase} setup failed", result.error.message)
                        self.assertEqual(result.steps, [])
                    finally:
                        if phase == "timeouts":
                            context.new_page = original
                        await context.close()

    async def test_cleanup_failure_preserves_original_execution_error(self):
        async with launched_browser() as browser:
            context = await browser.new_context()
            async def fail_step(*args, **kwargs):
                raise RuntimeError("execution evidence failed")
            async def fail_close():
                raise RuntimeError("context close failed")
            try:
                with patch.object(browser, "new_context", return_value=context), patch.object(
                    context, "close", fail_close
                ), patch("loop_worker.runner._run_step", fail_step):
                    result = await run_case(
                        build_case([goto_step(0, "https://example.test/", "example.test")]),
                        browser=browser, session_id="sess_cleanup",
                    )
                self.assertEqual(result.status, "error")
                self.assertIn("execution evidence failed", result.error.message)
                self.assertIn("cleanup_uncertain", result.error.message)
            finally:
                await context.close()
