# PPT 排版优化设计

## 问题

现有 `AI_Web_Testing_项目展示_评审阅读版_v3_中文优化.pptx` 存在：
1. 文字溢出框外
2. 图片与文字重叠
3. 字体单一无层次
4. 内容密度不均（过密或过稀）

## 方案

新写 `tools/build_ppt_from_outline.py`，读取 `decks/ai-web-testing-review/outline.json` + `design_brief.json` 作为内容源，python-pptx 生成。

### 文字溢出修复
- 动态字号：标题 >40 字符降 2pt，>60 字符降 4pt
- 文本框内边距 0.15in 保底
- 表格列宽按权重自适应，行高按内容自动计算
- 所有 text_frame.word_wrap = True

### 图文重叠修复
- image-sidebar 页：固定 55/45 分栏，0.3in 安全间距
- 图片先放置确定边界，文字区后放置避开
- 生成后坐标校验，重叠自动偏移

### 字体层次
- H1: YaHei Bold 28-30pt
- H2: YaHei Regular 14-16pt
- Body: YaHei Regular 12-14pt
- Caption: YaHei Light 10-11pt
- Takeaway: YaHei Bold 14pt

### 密度控制
- 封面 40% 面积，内容页 50-65%
- 图片页模板：左图 + 右 3 卡片
- 表格上限 5-6 行
- 要点块上限 3 个

### 7 种 slide renderer
title, split, table, image-sidebar, comparison-2col, timeline, standard

### 输出
`decks/ai-web-testing-review/build/ai-web-testing-v4.pptx`
