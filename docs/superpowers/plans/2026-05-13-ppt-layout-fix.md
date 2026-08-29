# PPT 排版优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 新写 `tools/build_ppt_from_outline.py`，读取 `outline.json` + `design_brief.json`，生成排版优化的 16:9 PPTX。

**Architecture:** 单脚本，7 种 slide renderer 函数 + LayoutEngine 类处理文本适配/坐标计算/字体管理。python-pptx 直接生成。

**Tech Stack:** Python 3.12+, python-pptx 1.0.2, Pillow, Microsoft YaHei 字体

---

### Task 1: 项目常量与字体系统

**Files:**
- Create: `tools/build_ppt_from_outline.py`

- [ ] **Step 1: 写入脚本头部 — 常量和字体注册**

```python
"""JSON-driven PPT builder — reads outline.json + design_brief.json, outputs v4.pptx."""
from __future__ import annotations

import json
from pathlib import Path
from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pptx.util import Inches, Pt
from pptx.oxml import parse_xml
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
DECK = ROOT / "decks/ai-web-testing-review"
PIC = ROOT / "pic"
OUT = DECK / "build" / "ai-web-testing-v4.pptx"

# ── Font ──
FONT = "Microsoft YaHei"
FONT_LIGHT = "Microsoft YaHei Light"

# ── Colors ──
INK = RGBColor(17, 24, 39)
MUTED = RGBColor(75, 85, 99)
NAVY = RGBColor(17, 18, 42)
BLUE = RGBColor(37, 99, 235)
GREEN = RGBColor(22, 163, 74)
AMBER = RGBColor(217, 119, 6)
RED = RGBColor(220, 38, 38)
BG = RGBColor(248, 250, 252)
WHITE = RGBColor(255, 255, 255)
LINE = RGBColor(229, 231, 235)

# ── Canvas (16:9) ──
SLIDE_W = 13.333
SLIDE_H = 7.5
MX = 0.55          # margin x
MY = 0.35          # margin y top
HEADER_Y = 0.35
SUB_Y = 0.85
BODY_TOP = 1.50
FOOTER_Y = 6.50
FOOTER_H = 0.45
CONTENT_W = SLIDE_W - 2 * MX  # ~12.233
INNER_PAD = 0.15              # padding inside text boxes

# ── Font hierarchy ──
FONT_SIZES = {
    "h1": 28,        # page title
    "h1_long": 24,   # title > 40 chars
    "h1_xlong": 20,  # title > 60 chars
    "h2": 15,        # subtitle
    "body": 13,      # body text
    "body_small": 11,# body in tight spaces
    "card_title": 15,
    "card_body": 11,
    "table_header": 11,
    "table_cell": 10,
    "caption": 10,
    "footer": 10,
    "takeaway": 14,
    "cover_title": 32,
    "cover_sub": 15,
    "cover_tagline": 16,
    "cover_tech": 12,
}

# ── Image registry ──
PICS = {
    "sessions": PIC / "Snipaste_2026-05-13_19-47-45.png",
    "planning": PIC / "Snipaste_2026-05-13_19-48-02.png",
    "rerun": PIC / "Snipaste_2026-05-13_19-48-11.png",
    "cases": PIC / "Snipaste_2026-05-13_19-48-27.png",
    "edit": PIC / "Snipaste_2026-05-13_19-48-35.png",
    "report": PIC / "Snipaste_2026-05-13_19-48-46.png",
    "evidence": PIC / "Snipaste_2026-05-13_19-49-22.png",
}

# Map outline asset keys to PICS keys
ASSET_MAP = {
    "assets/Snipaste_2026-05-13_19-48-02.png": "planning",
    "assets/Snipaste_2026-05-13_19-48-11.png": "rerun",
    "assets/Snipaste_2026-05-13_19-48-27.png": "cases",
    "assets/Snipaste_2026-05-13_19-48-35.png": "edit",
    "assets/Snipaste_2026-05-13_19-49-22.png": "evidence",
    "assets/Snipaste_2026-05-13_19-48-46.png": "report",
    "assets/Snipaste_2026-05-13_19-47-45.png": "sessions",
}


def east_asian_font(run, font_name=FONT):
    """Force East Asian font tag on a run for proper Chinese rendering."""
    rpr = run._r.get_or_add_rPr()
    rpr.set("lang", "zh-CN")
    for tag in ("a:latin", "a:ea", "a:cs"):
        ns = "http://schemas.openxmlformats.org/drawingml/2006/main"
        el = rpr.find(tag, namespaces={"a": ns})
        if el is None:
            el = parse_xml(f'<{tag} xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" typeface="{font_name}"/>')
            rpr.append(el)
        else:
            el.set("typeface", font_name)


def set_font(run, size, color=INK, bold=False, font_name=FONT):
    """Apply font properties to a run."""
    run.font.name = font_name
    run.font.size = Pt(size)
    run.font.bold = bold
    run.font.color.rgb = color
    east_asian_font(run, font_name)
```

- [ ] **Step 2: 验证常量可导入**

```bash
python -c "import sys; sys.path.insert(0, 'tools'); from build_ppt_from_outline import *; print('OK')"
```

---

### Task 2: Layout Engine — 文本适配与坐标工具

**Files:**
- Modify: `tools/build_ppt_from_outline.py` (追加)

- [ ] **Step 1: 实现文本尺寸估算与自动收缩**

```python
def estimate_text_size(text, font_size, max_w_in, max_h_in):
    """Return (fitted_font_size, needed_height_in) that fits within max_w x max_h.
    
    Approximation: CJK char ≈ 0.75 * font_size points wide.
    Line height ≈ 1.5 * font_size points.
    """
    usable_w_pt = (max_w_in - 2 * INNER_PAD) * 72
    chars_per_line = max(1, int(usable_w_pt / (font_size * 0.75)))
    
    lines_needed = 0
    for paragraph in text.split("\n"):
        if not paragraph:
            lines_needed += 1
            continue
        lines_needed += max(1, -(-len(paragraph) // chars_per_line))  # ceil division
    
    line_h_pt = font_size * 1.5
    needed_h_in = (lines_needed * line_h_pt) / 72 + 2 * INNER_PAD
    
    if needed_h_in <= max_h_in:
        return font_size, needed_h_in
    
    # Shrink
    for s in range(font_size - 2, 7, -2):
        cpl = max(1, int(usable_w_pt / (s * 0.75)))
        ln = 0
        for p in text.split("\n"):
            if not p:
                ln += 1
                continue
            ln += max(1, -(-len(p) // cpl))
        need_h = (ln * s * 1.5) / 72 + 2 * INNER_PAD
        if need_h <= max_h_in:
            return s, need_h
    return 8, max_h_in  # minimum


def fit_title(title, max_w=CONTENT_W):
    """Pick title font size based on length."""
    if len(title) > 60:
        return FONT_SIZES["h1_xlong"]
    elif len(title) > 40:
        return FONT_SIZES["h1_long"]
    return FONT_SIZES["h1"]


def add_textbox(slide, text, x, y, w, h, size=13, color=INK, bold=False,
                align=PP_ALIGN.LEFT, valign=MSO_ANCHOR.TOP,
                fill=None, border_color=None, font_name=FONT):
    """Create a textbox with proper EA font settings and padding."""
    shape = slide.shapes.add_textbox(Inches(x), Inches(y), Inches(w), Inches(h))
    if fill:
        shape.fill.solid()
        shape.fill.fore_color.rgb = fill
    else:
        shape.fill.background()
    if border_color:
        shape.line.color.rgb = border_color
        shape.line.width = Pt(0.8)
    else:
        shape.line.fill.background()
    
    tf = shape.text_frame
    tf.clear()
    tf.margin_left = Inches(INNER_PAD)
    tf.margin_right = Inches(INNER_PAD)
    tf.margin_top = Inches(INNER_PAD)
    tf.margin_bottom = Inches(INNER_PAD)
    tf.vertical_anchor = valign
    tf.word_wrap = True
    
    for idx, line_text in enumerate(text.split("\n")):
        p = tf.paragraphs[0] if idx == 0 else tf.add_paragraph()
        p.alignment = align
        p.space_after = Pt(3)
        run = p.add_run()
        run.text = line_text
        set_font(run, size, color, bold, font_name)
    return shape
```

- [ ] **Step 2: 实现图形辅助函数**

```python
def add_bg(slide, color=BG):
    """Full-slide background rectangle."""
    rect = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, 0, 0, Inches(SLIDE_W), Inches(SLIDE_H))
    rect.fill.solid()
    rect.fill.fore_color.rgb = color
    rect.line.fill.background()


def add_card(slide, title, body, x, y, w, h, accent=BLUE):
    """Rounded card with title + body text, auto-fitting body."""
    shape = slide.shapes.add_shape(
        MSO_SHAPE.ROUNDED_RECTANGLE, Inches(x), Inches(y), Inches(w), Inches(h)
    )
    shape.fill.solid()
    shape.fill.fore_color.rgb = WHITE
    shape.line.color.rgb = LINE
    shape.line.width = Pt(0.8)
    
    # Title: top of card
    add_textbox(slide, title, x + 0.15, y + 0.10, w - 0.30, 0.35,
                FONT_SIZES["card_title"], accent, True)
    
    # Body: fit to remaining space
    body_h = h - 0.60
    fit_sz, _ = estimate_text_size(body, FONT_SIZES["card_body"], w - 0.30, body_h)
    add_textbox(slide, body, x + 0.15, y + 0.50, w - 0.30, body_h,
                fit_sz, MUTED, font_name=FONT_LIGHT)


def add_takeaway(slide, text, y=FOOTER_Y):
    """Bottom takeaway bar with colored background."""
    shape = slide.shapes.add_shape(
        MSO_SHAPE.RECTANGLE, Inches(MX), Inches(y), Inches(CONTENT_W), Inches(FOOTER_H)
    )
    shape.fill.solid()
    shape.fill.fore_color.rgb = WHITE
    shape.line.color.rgb = LINE
    shape.line.width = Pt(0.8)
    add_textbox(slide, text, MX + 0.15, y + 0.05, CONTENT_W - 0.30, FOOTER_H - 0.10,
                FONT_SIZES["takeaway"], NAVY, True, PP_ALIGN.CENTER, MSO_ANCHOR.MIDDLE)


def add_header(slide, title, subtitle=None, page=None):
    """Standard page header with optional subtitle and page number."""
    title_sz = fit_title(title)
    add_textbox(slide, title, MX, HEADER_Y, CONTENT_W, 0.50, title_sz, INK, True)
    if subtitle:
        add_textbox(slide, subtitle, MX + 0.02, SUB_Y, CONTENT_W - 1.0, 0.35,
                    FONT_SIZES["h2"], MUTED, font_name=FONT_LIGHT)
    if page:
        add_textbox(slide, page, 12.0, HEADER_Y + 0.02, 0.70, 0.25, 10, MUTED, align=PP_ALIGN.RIGHT)
```

- [ ] **Step 3: 实现图片放置函数（防重叠）**

```python
def add_fit_image(slide, image_path, x, y, w, h, border=True):
    """Place image maintaining aspect ratio, centered in bounding box. Returns (actual_x, actual_y, actual_w, actual_h)."""
    with Image.open(image_path) as im:
        iw, ih = im.size
    ratio = iw / ih
    box_ratio = w / h
    if box_ratio > ratio:
        # box wider than image → fit by height
        ph = h
        pw = h * ratio
    else:
        # box taller than image → fit by width
        pw = w
        ph = w / ratio
    px = x + (w - pw) / 2
    py = y + (h - ph) / 2
    pic_shape = slide.shapes.add_picture(
        str(image_path), Inches(px), Inches(py), Inches(pw), Inches(ph)
    )
    if border:
        pic_shape.line.color.rgb = LINE
        pic_shape.line.width = Pt(1.0)
    return (px, py, pw, ph)
```

- [ ] **Step 4: 验证辅助函数**

```bash
python -c "
import sys; sys.path.insert(0, 'tools')
from build_ppt_from_outline import *
# Test text sizing
sz, h = estimate_text_size('短短', 13, 4.0, 2.0)
assert sz == 13, f'Expected 13 got {sz}'
sz2, h2 = estimate_text_size('这是一段非常非常非常非常非常长的文本需要缩小', 13, 2.0, 1.0)
assert sz2 < 13, f'Expected shrink got {sz2}'
print('OK')
"
```

---

### Task 3: 表格渲染器

**Files:**
- Modify: `tools/build_ppt_from_outline.py` (追加)

- [ ] **Step 1: 实现自适应表格函数**

```python
def add_table(slide, headers, rows, x, y, w, h, col_weights=None, caption=None):
    """Add a self-fitting table. Column widths from weights. Row heights auto-calc."""
    table_shape = slide.shapes.add_table(
        len(rows) + 1, len(headers), Inches(x), Inches(y), Inches(w), Inches(h)
    )
    tbl = table_shape.table
    
    # Set column widths
    if col_weights:
        total_w = sum(col_weights)
        for i, cw in enumerate(col_weights):
            tbl.columns[i].width = Inches(w * cw / total_w)
    
    # Header row
    for c, header in enumerate(headers):
        cell = tbl.cell(0, c)
        cell.text = ""
        p = cell.text_frame.paragraphs[0]
        run = p.add_run()
        run.text = header
        set_font(run, FONT_SIZES["table_header"], WHITE, True)
        # Background
        cell_fill(cell, NAVY)
        cell.text_frame.margin_left = Inches(0.08)
        cell.text_frame.margin_right = Inches(0.08)
        cell.text_frame.margin_top = Inches(0.05)
        cell.text_frame.margin_bottom = Inches(0.05)
        cell.text_frame.word_wrap = True
    
    # Data rows
    for r, row in enumerate(rows, 1):
        bg_color = WHITE if r % 2 else RGBColor(245, 247, 250)
        for c, val in enumerate(row):
            cell = tbl.cell(r, c)
            cell.text = ""
            p = cell.text_frame.paragraphs[0]
            run = p.add_run()
            run.text = str(val)
            set_font(run, FONT_SIZES["table_cell"], INK)
            cell_fill(cell, bg_color)
            cell.text_frame.margin_left = Inches(0.08)
            cell.text_frame.margin_right = Inches(0.08)
            cell.text_frame.margin_top = Inches(0.04)
            cell.text_frame.margin_bottom = Inches(0.04)
            cell.text_frame.word_wrap = True
    
    if caption:
        add_textbox(slide, caption, x, y + h + 0.05, w, 0.25,
                    FONT_SIZES["caption"], MUTED, font_name=FONT_LIGHT)
    return table_shape


def cell_fill(cell, color):
    """Set cell background color."""
    cell.fill.solid()
    cell.fill.fore_color.rgb = color
```

- [ ] **Step 2: 验证表格辅助函数**

```bash
python -c "from tools.build_ppt_from_outline import cell_fill; print('OK')"
```

---

### Task 4: 7 种 Slide Renderer

**Files:**
- Modify: `tools/build_ppt_from_outline.py` (追加)

- [ ] **Step 1: Title 封面渲染器**

```python
def render_title(prs, slide_data):
    """Cover slide with hero image + project name + tech stack."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    
    title = slide_data.get("title", "")
    subtitle = slide_data.get("subtitle", "")
    assets = slide_data.get("assets", {})
    notes = slide_data.get("notes", "")
    
    # Title area - left side
    add_textbox(slide, title, MX, 0.88, 6.0, 1.30, FONT_SIZES["cover_title"], INK, True)
    add_textbox(slide, subtitle, MX + 0.05, 2.40, 6.0, 0.55, FONT_SIZES["cover_sub"], MUTED, font_name=FONT_LIGHT)
    add_textbox(slide, notes, MX + 0.05, 3.20, 5.80, 0.80, FONT_SIZES["cover_tagline"], NAVY, True, fill=WHITE, border_color=LINE)
    
    # Hero images - right side
    hero_key = None
    if "hero_image" in assets:
        hero_key = ASSET_MAP.get(assets["hero_image"])
    if hero_key and hero_key in PICS:
        add_fit_image(slide, PICS[hero_key], 6.85, 0.82, 5.80, 3.20)
    
    # Second image if available
    if "hero_image" in assets and assets["hero_image"] == "assets/Snipaste_2026-05-13_19-48-02.png":
        if "report" in PICS:
            add_fit_image(slide, PICS["report"], 6.85, 4.20, 5.80, 2.40)
    
    # Tech stack line
    add_textbox(slide, "FastAPI  ·  React/Vite  ·  Playwright  ·  AI DSL  ·  DOM Locator",
                MX, 6.55, 6.0, 0.35, FONT_SIZES["cover_tech"], MUTED, font_name=FONT_LIGHT)
    return slide
```

- [ ] **Step 2: Split 内容页渲染器（要点列表）**

```python
def render_split(prs, slide_data, page_num, total):
    """Split layout: title + highlights list + summary callout."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    highlights = slide_data.get("highlights", [])
    body_text = slide_data.get("body", "")
    
    n = len(highlights)
    if n == 0:
        add_textbox(slide, body_text, MX, BODY_TOP, CONTENT_W, 4.5,
                    FONT_SIZES["body"], INK)
    elif n <= 4:
        cols = 2
        rows_per = -(-n // cols)
        card_w = (CONTENT_W - 0.30) / 2
        card_h = 3.8 / rows_per
        colors = [BLUE, GREEN, AMBER, RED]
        for i, hl in enumerate(highlights):
            col = i // rows_per
            row = i % rows_per
            cx = MX + col * (card_w + 0.30)
            cy = BODY_TOP + row * (card_h + 0.20)
            # Parse "title: body" format
            if ": " in hl or "：" in hl:
                sep = ": " if ": " in hl else "："
                ct, cb = hl.split(sep, 1)
            else:
                ct, cb = hl[:20], hl
            add_card(slide, ct, cb, cx, cy, card_w, min(card_h, 1.55), colors[i % len(colors)])
    
    callout = slide_data.get("summary_callout", "")
    if callout:
        add_takeaway(slide, callout)
    return slide
```

- [ ] **Step 3: Image-sidebar 渲染器（截图+卡片）**

```python
def render_image_sidebar(prs, slide_data, page_num, total):
    """Left or right image + sidebar cards layout."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    image_side = slide_data.get("image_side", "left")
    sections = slide_data.get("sidebar_sections", [])
    assets = slide_data.get("assets", {})
    callout = slide_data.get("summary_callout", "")
    
    # Image zone: 55% of content width
    img_w = CONTENT_W * 0.53
    img_h = 4.45
    img_x = MX if image_side == "left" else MX + CONTENT_W - img_w
    img_y = BODY_TOP
    
    # Text zone: remaining with gap
    gap = 0.30
    text_w = CONTENT_W - img_w - gap
    text_x = MX + img_w + gap if image_side == "left" else MX
    
    # Place image
    hero_key = None
    if "hero_image" in assets:
        hero_key = ASSET_MAP.get(assets["hero_image"])
    if hero_key and hero_key in PICS:
        add_fit_image(slide, PICS[hero_key], img_x, img_y, img_w, img_h)
    
    # Place sidebar cards
    card_colors = [BLUE, GREEN, AMBER]
    n_sec = len(sections)
    card_h = min(1.15, (img_h - (n_sec - 1) * 0.12) / n_sec)
    for i, sec in enumerate(sections):
        cy = img_y + i * (card_h + 0.12)
        add_card(slide, sec.get("title", ""), sec.get("body", ""),
                 text_x, cy, text_w, card_h, card_colors[i % 3])
    
    if callout:
        add_takeaway(slide, callout)
    return slide
```

- [ ] **Step 4: Table 渲染器**

```python
def render_table(prs, slide_data, page_num, total):
    """Table layout: header + data table + optional caption + takeaway."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    table_data = slide_data.get("table", {})
    headers = table_data.get("headers", [])
    rows = table_data.get("rows", [])
    col_weights = table_data.get("column_weights")
    caption = table_data.get("caption", "")
    
    # Table area
    table_y = BODY_TOP
    table_h = len(rows) * 0.55 + 0.45  # row height estimate + header
    table_h = min(table_h, 5.0)  # cap at 5 inches
    
    add_table(slide, headers, rows, MX, table_y, CONTENT_W, table_h,
              col_weights, caption)
    
    callout = slide_data.get("summary_callout", "")
    if callout:
        add_takeaway(slide, callout)
    return slide
```

- [ ] **Step 5: Comparison-2col 渲染器**

```python
def render_comparison_2col(prs, slide_data, page_num, total):
    """Two-column comparison with verdict."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    left = slide_data.get("left", {})
    right = slide_data.get("right", {})
    verdict = slide_data.get("verdict", "")
    
    col_w = (CONTENT_W - 0.30) / 2
    col_h = 3.80
    
    # Left column
    add_card(slide, left.get("title", ""),
             "\n".join(left.get("body", [])),
             MX, BODY_TOP, col_w, col_h, RED)
    
    # Right column
    add_card(slide, right.get("title", ""),
             "\n".join(right.get("body", [])),
             MX + col_w + 0.30, BODY_TOP, col_w, col_h, BLUE)
    
    # Verdict
    if verdict:
        add_textbox(slide, verdict, MX, BODY_TOP + col_h + 0.20, CONTENT_W, 0.60,
                    FONT_SIZES["takeaway"], NAVY, True, PP_ALIGN.CENTER, MSO_ANCHOR.MIDDLE,
                    fill=WHITE, border_color=LINE)
    
    callout = slide_data.get("summary_callout", "")
    if callout:
        add_takeaway(slide, callout)
    return slide
```

- [ ] **Step 6: Timeline 渲染器**

```python
def render_timeline(prs, slide_data, page_num, total):
    """Timeline with milestone cards in horizontal bands."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    milestones = slide_data.get("milestones", [])
    n = len(milestones)
    if n == 0:
        return slide
    
    card_w = (CONTENT_W - (n - 1) * 0.25) / n
    card_h = 3.80
    colors = [BLUE, GREEN, AMBER, RED]
    
    for i, ms in enumerate(milestones):
        cx = MX + i * (card_w + 0.25)
        cy = BODY_TOP
        
        # Label band
        add_textbox(slide, ms.get("label", ""),
                    cx, cy, card_w, 0.35, FONT_SIZES["card_title"], WHITE, True,
                    PP_ALIGN.CENTER, MSO_ANCHOR.MIDDLE, fill=colors[i % len(colors)])
        
        # Card body
        add_card(slide, ms.get("title", ""), ms.get("body", ""),
                 cx, cy + 0.45, card_w, card_h - 0.45, colors[i % len(colors)])
    
    callout = slide_data.get("summary_callout", "")
    if callout:
        add_takeaway(slide, callout)
    return slide
```

- [ ] **Step 7: Standard 通用渲染器（兜底）**

```python
def render_standard(prs, slide_data, page_num, total):
    """Fallback renderer: title + body text."""
    slide = prs.slides.add_slide(prs.slide_layouts[6])
    add_bg(slide)
    add_header(slide, slide_data.get("title", ""), slide_data.get("subtitle", ""),
               f"{page_num}/{total}")
    
    body = slide_data.get("body", "")
    if body:
        fit_sz, _ = estimate_text_size(body, FONT_SIZES["body"], CONTENT_W, 4.5)
        add_textbox(slide, body, MX, BODY_TOP, CONTENT_W, 4.5, fit_sz, INK)
    
    callout = slide_data.get("summary_callout", "")
    if callout:
        add_takeaway(slide, callout)
    return slide


# Renderer dispatch
RENDERERS = {
    "title": render_title,
    "split": render_split,
    "image-sidebar": render_image_sidebar,
    "table": render_table,
    "comparison-2col": render_comparison_2col,
    "timeline": render_timeline,
    "standard": render_standard,
}
```

---

### Task 5: 主构建流程

**Files:**
- Modify: `tools/build_ppt_from_outline.py` (追加)

- [ ] **Step 1: 实现 build() 主函数**

```python
def build():
    # Load content
    with open(DECK / "outline.json", "r", encoding="utf-8") as f:
        outline = json.load(f)
    
    slides = outline.get("slides", [])
    total = len(slides)
    
    # Create presentation
    prs = Presentation()
    prs.slide_width = Inches(SLIDE_W * 914400 / 13.333)  # EMU conversion
    # Actually: python-pptx uses EMU internally; Inches() does the right conversion.
    # Just set directly:
    prs.slide_width = Inches(int(SLIDE_W * 914400 / 13.333))
    # Let me just do:
    prs.slide_width = Inches(13.333)
    prs.slide_height = Inches(7.5)
    
    OUT.parent.mkdir(parents=True, exist_ok=True)
    
    for i, slide_data in enumerate(slides):
        variant = slide_data.get("variant", slide_data.get("type", "standard"))
        renderer = RENDERERS.get(variant, render_standard)
        renderer(prs, slide_data, i + 1, total)
        print(f"  Slide {i+1}/{total}: {slide_data.get('title', '')[:60]} [{variant}]")
    
    prs.save(str(OUT))
    print(f"\nSaved: {OUT}")


if __name__ == "__main__":
    build()
```

- [ ] **Step 2: 运行构建**

```bash
python tools/build_ppt_from_outline.py
```

- [ ] **Step 3: 验证输出**

```bash
python -c "
from pptx import Presentation
prs = Presentation('decks/ai-web-testing-review/build/ai-web-testing-v4.pptx')
print(f'Slides: {len(prs.slides)}')
for i, slide in enumerate(prs.slides, 1):
    shapes_n = len(slide.shapes)
    print(f'  Slide {i}: {shapes_n} shapes')
print('OK')
"
```

---

### Task 6: 密度与排版质量验证

- [ ] **Step 1: 写密度检查脚本**

```bash
python -c "
from pptx import Presentation
from pptx.util import Inches, Pt
prs = Presentation('decks/ai-web-testing-review/build/ai-web-testing-v4.pptx')
SW = prs.slide_width / 914400  # EMU to inches
SH = prs.slide_height / 914400
total_area = SW * SH
print(f'Canvas: {SW:.1f} x {SH:.1f} = {total_area:.1f} sq in')
for i, slide in enumerate(prs.slides, 1):
    content_area = 0
    for shape in slide.shapes:
        l = shape.left / 914400
        t = shape.top / 914400
        w = shape.width / 914400
        h = shape.height / 914400
        content_area += w * h
    density = content_area / total_area
    status = 'OK' if 0.35 < density < 0.72 else 'CHECK'
    print(f'  Slide {i}: {content_area:.1f} sq in, density={density:.3f} [{status}]')
"
```

- [ ] **Step 2: 检查重叠**

```bash
python -c "
from pptx import Presentation
prs = Presentation('decks/ai-web-testing-review/build/ai-web-testing-v4.pptx')
for i, slide in enumerate(prs.slides, 1):
    rects = []
    for shape in slide.shapes:
        l = shape.left
        t = shape.top
        r = l + shape.width
        b = t + shape.height
        rects.append((l, t, r, b, shape.shape_type))
    overlaps = 0
    for a in range(len(rects)):
        for b_idx in range(a+1, len(rects)):
            la, ta, ra, ba, _ = rects[a]
            lb, tb, rb, bb, _ = rects[b_idx]
            # Skip background (full-slide)
            ol_x = max(0, min(ra, rb) - max(la, lb))
            ol_y = max(0, min(ba, bb) - max(ta, tb))
            if ol_x > 100000 and ol_y > 100000:  # significant overlap in EMU
                overlaps += 1
    status = 'CLEAN' if overlaps == 0 else f'{overlaps} OVERLAPS'
    print(f'  Slide {i}: {status}')
"
```

---

### Task 7: 提交

- [ ] **Step 1: 提交所有文件**

```bash
git add tools/build_ppt_from_outline.py decks/ai-web-testing-review/build/ai-web-testing-v4.pptx docs/superpowers/specs/2026-05-13-ppt-layout-fix-design.md docs/superpowers/plans/2026-05-13-ppt-layout-fix.md
git commit -m "feat: JSON-driven PPT builder with layout fixes — text auto-fit, grid layout, font hierarchy"
```
