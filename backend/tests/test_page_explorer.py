from __future__ import annotations

import unittest

from app.ai.page_explorer import _filter_a11y_nodes


class PageExplorerA11yFilterTest(unittest.TestCase):
    def test_filter_excludes_non_targetable_document_nodes(self) -> None:
        nodes = [
            {
                "nodeId": "root",
                "role": {"value": "RootWebArea"},
                "name": {"value": "Example Domain"},
            },
            {
                "nodeId": "heading",
                "role": {"value": "heading"},
                "name": {"value": "Example Domain"},
            },
            {
                "nodeId": "text",
                "role": {"value": "StaticText"},
                "name": {"value": "Example Domain"},
            },
        ]

        filtered = _filter_a11y_nodes(nodes)

        self.assertEqual(["heading"], [node["nodeId"] for node in filtered])


if __name__ == "__main__":
    unittest.main()
