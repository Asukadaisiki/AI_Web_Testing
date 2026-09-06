from __future__ import annotations

from collections import Counter
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
import threading
import unittest
from urllib.parse import urlsplit

from app.ai.page_explorer import _collect_flow_a11y


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


if __name__ == "__main__":
    unittest.main()
