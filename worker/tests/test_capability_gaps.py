"""能力边界取证测试（F1 / F2 / F5、D12）—— 夹具页 `capability.html`。

**这些断言固定的是"当前行为"，不是"正确行为"。**

每一条都标注了它对应 `plan/09-experiment-log.md` 的哪条发现，以及为什么它现在是这样。
修好之后应当把这些断言**翻过来**（而不是删掉），这样"边界是否真的被突破"才有回归依据。

⚠️ 读这份文件前先知道一件事：私有使用区字形（U+E000–U+F8FF，图标字体用的码位）
**在终端和编辑器里不可见**。把它们 dump 成 JSON 再肉眼看，会误以为字段是空串。
所以这里一律用 `\\uf002` 这样的转义写，不要用字面字符。

证据来源：对 `capability.html` 的真实观测，可用 `tests/_explore_capability.py` 重新生成。
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from local_site import LocalSite  # noqa: E402
from pw_support import launched_browser, run  # noqa: E402

from loop_worker.observer import observe_page  # noqa: E402

#: Font Awesome 搜索图标（U+F002）——纯字形按钮的内容
SEARCH_GLYPH = "\uf002"

#: Font Awesome 购物车图标（U+F07A）——字形 + 文本混排
CART_GLYPH = "\uf07a"

#: 同名链接的文本（F2）
DUPLICATE_LINK_TEXT = "View Product"


def css_locators(element) -> list[str]:  # noqa: ANN001
    return [locator.css for locator in element.locators if locator.kind == "css"]


def kinds(element) -> list[str]:  # noqa: ANN001
    return [locator.kind for locator in element.locators]


def by_css(observation, css: str):  # noqa: ANN001
    """按已验证的 css 定位器取元素；取不到返回 None。"""
    for element in observation.elements:
        if css in css_locators(element):
            return element
    return None


def by_text(observation, text: str) -> list:  # noqa: ANN001
    return [element for element in observation.elements if element.text == text]


class CapabilityGapTest(unittest.TestCase):
    """对 capability.html 的一次观测，逐条核对已知能力边界。"""

    observation = None

    @classmethod
    def setUpClass(cls) -> None:
        cls.observation = run(cls._observe())

    @classmethod
    async def _observe(cls):  # noqa: ANN206
        with LocalSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()
                await page.goto(site.url("capability.html"))
                return await observe_page(page)

    # ------------------------------------------------------------------
    # F1：图标按钮在契约里无法定位（阻塞级）
    # ------------------------------------------------------------------

    def test_icon_only_button_text_is_a_bare_glyph(self) -> None:
        """F1：纯字形按钮的**唯一内容就是一个不可见的私有区码位**，且拿不到可访问名。

        - `text` = `"\\uf002"`：渲染不出字形的码位仍然进了文本面；
        - `name` = None：`accessible_name` 只在 `get_by_role(role, name=...)` 唯一命中时
          才返回名字（`observer.py:230`）。本页有两个 `\\uf002` 按钮（`#icon-only` 与
          `#search-submit`），互相撞车 ⇒ 两个都拿不到名字。
          （真实站点上只有一个时名字会是 `"\\uf002"`——**那也等于没有名字**，
          因为模型不可能在 hint 里打出这个不可见码位。）
        - 只剩一条绝对 css 定位器。

        后果：Go 侧解析器只按 `name` / `text` 打分（`resolve.go:186`），
        两个面要么为空、要么是不可见码位 ⇒ **任何自然语言 hint 都命中不到它**。
        这正是 plan/09 F1 说的"真实站点上'在页面里搜索'这一步做不到"。
        """
        element = by_css(self.observation, "#icon-only")
        self.assertIsNotNone(element, "fixture 页应当观测到 #icon-only")

        self.assertEqual(element.text, SEARCH_GLYPH)
        self.assertIsNone(element.name, "同名字形按钮互相撞车，应当拿不到可访问名")
        self.assertEqual(kinds(element), ["css"], "纯字形按钮只应剩 css 定位器")

    def test_icon_only_search_submit_is_equally_unreachable(self) -> None:
        """F1：搜索提交按钮与纯字形按钮同病——模型无法用任何 hint 指到它。

        这条是 F1 的**实际卡点**：搜索框本身（`#search-input`）有 placeholder 派生的
        干净名字 `"Search items"`，是能定位的；卡住的是**提交按钮**。
        """
        submit = by_css(self.observation, "#search-submit")
        self.assertIsNotNone(submit, "fixture 页应当观测到 #search-submit")

        self.assertEqual(submit.text, SEARCH_GLYPH)
        self.assertIsNone(submit.name)
        self.assertEqual(kinds(submit), ["css"])

        # 对照组：搜索框本身是能定位的，所以卡点确实只在提交按钮上。
        search_input = by_css(self.observation, "#search-input")
        self.assertIsNotNone(search_input)
        self.assertEqual(search_input.name, "Search items")
        self.assertIn("role", kinds(search_input))

    # ------------------------------------------------------------------
    # F5：可访问名被图标字形污染
    # ------------------------------------------------------------------

    def test_icon_plus_text_keeps_the_glyph_in_its_name(self) -> None:
        """F5：字形**没有**被剥离，可访问名与文本都带着它。

        实测 `name` = `text` = `"\\uf07a Cart"`（与 plan/09 F5 记录的 `"\\uf07a Cart"` 一致）。

        对解析器的影响（`resolve.go:186` 的打分表）：
        - `hint="Cart"` ⇒ 精确匹配失败（`"\\uf07a cart" != "cart"`），
          落到**子串匹配 80 分**（`contains("\\uf07a cart", "cart")` 为真）⇒ 仍然定位得到；
        - 所以 F5 单独不会阻断定位，**但精确匹配这一档在这些元素上永远不成立**，
          一旦页面上还有别的元素也含 "cart"，80 分档就会并列 ⇒ `target_ambiguous`。
        """
        element = by_css(self.observation, "#icon-plus-text")
        self.assertIsNotNone(element, "fixture 页应当观测到 #icon-plus-text")

        self.assertEqual(element.name, f"{CART_GLYPH} Cart")
        self.assertEqual(element.text, f"{CART_GLYPH} Cart")

    def test_aria_hidden_icon_yields_a_clean_name(self) -> None:
        """F5 的正确写法已经被正确处理：`aria-hidden` 让字形不进可访问名。

        这条是**正向**断言：观测侧对"作者写对了"的情况没有问题。
        所以 F5 的修复方向不是改观测侧的计算，而是让"作者没写对"的元素也能被指到。
        """
        element = by_css(self.observation, "#icon-hidden")
        self.assertIsNotNone(element, "fixture 页应当观测到 #icon-hidden")

        self.assertEqual(element.name, "Save")
        self.assertEqual(element.text, "Save")
        self.assertIn("role", kinds(element), "名字干净的元素应当有 role 定位器")

    # ------------------------------------------------------------------
    # F2：N 个完全相同的元素无法区分（阻塞级）
    # ------------------------------------------------------------------

    def test_duplicate_links_lose_their_name_entirely(self) -> None:
        """F2：三个同名链接的名字被**整体丢弃**，只剩绝对 css 定位器。

        `accessible_name` 要求 role 定位器唯一命中（`observer.py:230`）；
        `get_by_role("link", name="View Product")` 命中 3 个 ⇒ 返回 None。
        设计意图是"绝不宣传一个不能当定位器用的名字"，但它有个副作用：
        **模型连 hint 都拿不到**，只能看到三条一模一样的 `text`。

        后果：`resolve` 给三条都打 95 分 ⇒ `target_ambiguous` ⇒ 拒绝执行（`resolve.go:165`）。
        拒绝是对的（v1 会默默点第一个），但结论是"按名字定位卡片内元素"现在不可表达。
        """
        links = by_text(self.observation, DUPLICATE_LINK_TEXT)
        self.assertEqual(len(links), 3, "fixture 页应当观测到三个同名链接")

        for element in links:
            self.assertIsNone(element.name, "同名元素的名字应当被整体丢弃")
            self.assertEqual(kinds(element), ["css"], "同名元素只应剩 css 定位器")

    def test_card_container_is_not_observed_at_all(self) -> None:
        """F2 / D9 的**真正**根因：容器根本没进观测，所以作用域没有可挂靠的锚点。

        `div.card` 非交互、没有自有文本、但有子元素 ⇒ 被采集条件跳过
        （`observer.py:128`：`!interactive && own.length === 0 && !(children.length === 0 && ...)`）。

        这条是本次取证**新发现**的：plan/09 把 F2 归因于"商品名在 <p> 里观测不收"，
        但实测商品名 `<p>` 是收的（见下一条）。真正缺的是**元素之间的结构关系**——
        观测是一张扁平表，卡片容器不在表里，"Blue Top 卡片里的 View Product"无从表达。

        所以 D9（容器内相对定位）不能只加一个 `within` 参数就完事：
        **必须先把"容器"这类结构节点收进观测**，否则 `within` 解析不到任何东西。
        """
        container_css = "#cards > div:nth-of-type(1)"
        self.assertIsNone(
            by_css(self.observation, container_css),
            "卡片容器当前不应出现在观测里（这条断言是 D9 的施工前提）",
        )

        # 更一般的说法：观测里没有任何"带子元素但自身无文本"的结构容器。
        for element in self.observation.elements:
            self.assertFalse(
                element.tag == "div" and not (element.text or "").strip(),
                f"不应观测到空文本的结构容器，但遇到了 {element.ref} {css_locators(element)}",
            )

    # ------------------------------------------------------------------
    # D12：观测是否收录非交互文本
    # ------------------------------------------------------------------

    def test_non_interactive_text_is_already_observed(self) -> None:
        """D12 的答案：**非交互文本已经收进观测了**，这条不需要补做。

        与 plan/09 的 F2 记录相反：`<p>Blue Top</p>` 确实在观测里，还带一条
        `match_count == 1` 的 text 定位器。`<p id="plain-note">`、`<div id="plain-block">`
        同样都在。

        因此 D12 的正确结论不是"补收录非交互文本"，而是：
        收录了**但没有把它和附近的交互元素关联起来**。缺口在结构，不在收录。
        """
        for css, expected_text in (
            ("#plain-note", "Plain paragraph text"),
            ("#plain-block", "Block text without any interactive child"),
        ):
            element = by_css(self.observation, css)
            self.assertIsNotNone(element, f"{css} 应当被观测到")
            self.assertEqual(element.text, expected_text)
            self.assertIn("text", kinds(element))

        for product_name in ("Blue Top", "Red Top", "Green Top"):
            matches = by_text(self.observation, product_name)
            self.assertEqual(
                len(matches), 1, f"商品名 {product_name!r} 应当被观测到且唯一"
            )
            self.assertIn("text", kinds(matches[0]))


if __name__ == "__main__":
    unittest.main()
