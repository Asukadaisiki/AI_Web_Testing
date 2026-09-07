from __future__ import annotations

from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from urllib.parse import urlsplit
from unittest.mock import patch

from app.ai.page_explorer import BrowserSessionManager, _collect_flow_a11y
from app.application.browser.service import (
    execute_browser_capability,
    shutdown_browser_capabilities,
)


class _FlowHandler(BaseHTTPRequestHandler):
    requests: Counter[str] = Counter()

    def do_GET(self) -> None:
        path = urlsplit(self.path).path
        type(self).requests[path] += 1
        if path in {"/product", "/other"}:
            body = """
<!doctype html>
<html>
  <body>
    <header><a id="header-cart" href="/cart">Cart</a></header>
    <main>
      <button id="add-to-cart" type="button">Add to cart</button>
      <div id="cartModal" hidden>
        <a href="/view_cart">View Cart</a>
      </div>
    </main>
    <script>
      document.querySelector("#add-to-cart").addEventListener("click", () => {
        document.querySelector("#cartModal").hidden = false;
      });
    </script>
  </body>
</html>
"""
        elif path == "/view_cart":
            body = "<!doctype html><html><body><h1>Modal cart destination</h1></body></html>"
        elif path == "/cart":
            body = "<!doctype html><html><body><h1>Header cart bypass</h1></body></html>"
        elif path == "/context":
            authenticated = "stage5_auth=1" in self.headers.get("Cookie", "")
            label = "Authenticated session" if authenticated else "Anonymous session"
            body = f"<!doctype html><html><body><button>{label}</button></body></html>"
        else:
            self.send_error(404)
            return
        encoded = body.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, _format: str, *_args: object) -> None:
        return


@unittest.skipUnless(
    os.getenv("RUN_BROWSER_INTEGRATION") == "1",
    "set RUN_BROWSER_INTEGRATION=1 to run real Chromium regression",
)
class ExploreFlowChromiumTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        _FlowHandler.requests.clear()
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), _FlowHandler)
        cls.server_thread = threading.Thread(
            target=cls.server.serve_forever,
            daemon=True,
        )
        cls.server_thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.shutdown()
        cls.server.server_close()
        cls.server_thread.join(timeout=5)

    def setUp(self) -> None:
        _FlowHandler.requests.clear()

    def tearDown(self) -> None:
        shutdown_browser_capabilities()

    def test_navigation_contract_and_same_url_modal_flow(self) -> None:
        product_url = f"{self.base_url}/product?sku=blue#details"
        result = _collect_flow_a11y(
            [
                {"url": f"{self.base_url}/product?sku=blue"},
                {"url": f"{self.base_url}/product?sku=red"},
                {"url": f"{self.base_url}/other?sku=red"},
                {
                    "url": product_url,
                    "description": "product modal",
                    "actions": [
                        {"action": "click", "target": "#add-to-cart"},
                    ],
                },
                {
                    "url": f"{self.base_url}/product?sku=blue#cart-modal",
                    "description": "product modal",
                    "actions": [
                        {
                            "action": "wait_for",
                            "target": '#cartModal a[href="/view_cart"]',
                        },
                        {
                            "action": "click",
                            "target": '#cartModal a[href="/view_cart"]',
                        },
                    ],
                },
            ],
            timeout_ms=5000,
        )

        self.assertTrue(all(entry["status"] == "success" for entry in result))
        self.assertEqual(_FlowHandler.requests["/product"], 3)
        self.assertEqual(_FlowHandler.requests["/other"], 1)
        self.assertEqual(_FlowHandler.requests["/view_cart"], 1)
        self.assertEqual(_FlowHandler.requests["/cart"], 0)
        self.assertEqual(len(result), 4)
        self.assertEqual(len({entry["page_state"] for entry in result}), 4)

        product_entry = next(
            entry for entry in result if "/product?sku=blue" in entry["url"]
        )
        cart_entry = next(
            entry for entry in result if entry["url"].endswith("/view_cart")
        )
        product_actions = product_entry["actions"]
        self.assertEqual(
            {
                (3, 0, "before"),
                (3, 0, "after"),
                (4, 0, "before"),
                (4, 0, "after"),
                (4, 1, "before"),
            },
            {
                (
                    action["step_index"],
                    action["action_index"],
                    action["phase"],
                )
                for action in product_actions
            },
        )
        self.assertTrue(
            all(
                action["url"] == product_url
                and action["page_state"] == product_entry["page_state"]
                for action in product_actions
            )
        )
        self.assertGreater(cart_entry["revision"], product_entry["revision"])
        self.assertEqual(cart_entry["actions"][0]["phase"], "after")
        self.assertEqual(cart_entry["actions"][0]["url"], cart_entry["url"])
        self.assertNotEqual(
            cart_entry["actions"][0]["page_state"],
            product_entry["page_state"],
        )

    def test_clean_context_skips_project_storage_state_between_sessions(self) -> None:
        project_id = 41
        normal_session_id = 4101
        clean_session_id = 4102

        with tempfile.TemporaryDirectory() as directory:
            storage_state_path = Path(directory) / f"{project_id}.json"
            storage_state_path.write_text(
                json.dumps(
                    {
                        "cookies": [
                            {
                                "name": "stage5_auth",
                                "value": "1",
                                "domain": "127.0.0.1",
                                "path": "/",
                                "expires": -1,
                                "httpOnly": False,
                                "secure": False,
                                "sameSite": "Lax",
                            }
                        ],
                        "origins": [],
                    }
                ),
                encoding="utf-8",
            )
            with patch(
                "app.application.browser.service._storage_state_path",
                return_value=str(storage_state_path),
            ):
                normal = execute_browser_capability(
                    None,
                    capability="explore_page",
                    project_id=project_id,
                    conversation_id=str(normal_session_id),
                    context={"clean_context": False},
                    arguments={"url": f"{self.base_url}/context"},
                )
                clean = execute_browser_capability(
                    None,
                    capability="explore_page",
                    project_id=project_id,
                    conversation_id=str(clean_session_id),
                    context={"clean_context": True},
                    arguments={"url": f"{self.base_url}/context"},
                )

            self.assertIn(
                "Authenticated session",
                {node.get("name") for node in normal["a11y_nodes"]},
            )
            self.assertIn(
                "Anonymous session",
                {node.get("name") for node in clean["a11y_nodes"]},
            )
            self.assertTrue(normal["context_evidence"]["storage_state_loaded"])
            self.assertFalse(clean["context_evidence"]["storage_state_loaded"])
            self.assertEqual(
                normal["context_evidence"]["planning_session_id"],
                normal_session_id,
            )
            self.assertEqual(
                clean["context_evidence"]["planning_session_id"],
                clean_session_id,
            )
            self.assertEqual(
                set(BrowserSessionManager._sessions),
                {normal_session_id, clean_session_id},
            )
            self.assertTrue(storage_state_path.exists())


if __name__ == "__main__":
    unittest.main()
