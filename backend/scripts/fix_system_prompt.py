"""Fix the ask_user confirmation rule and a broken placeholder in system prompt."""
from __future__ import annotations

path = r"D:\AutoTestingLearingProject\AI_Web_Testing\backend\app\ai\test_planning_prompts.py"
content = open(path, encoding="utf-8").read()
lines = content.split('\n')

# Replace lines 90-92 (0-indexed) with correct rules
# Line 90: ask_user confirmation rule
# Line 91: user confirms then generate
# Line 92: user says generate directly
new_block = [
    "\t- 当已收集到 4 项及以上信息时，信息已经充足。此时必须直接使用 `generate_plan` 输出测试方案，不要再用 `ask_user` 询问用户是否足够。",
    "\t- 如果用户已经说\"直接生成\"\"够了\"\"先给方案\"，立即使用 `generate_plan`。",
]

# Confirm we have the right indices
for i in range(89, 94):
    print(f"Line {i+1}: {'KW FOUND' if ('收集到' in lines[i] or '确认' in lines[i] or '直接生成' in lines[i]) else 'no match'}")

# Replace lines 90-92 with 2 new lines
before = lines[:90]  # lines 0-89
after = lines[93:]    # lines 93-end (skip original lines 90,91,92)
new_content = '\n'.join(before + new_block + after)
open(path, 'w', encoding='utf-8').write(new_content)

print("\nDone. New lines 91-92:")
for i in range(89, 94):
    try:
        print(f"  {i+1}: {new_content.split(chr(10))[i][:150]}")
    except:
        pass

# Also fix the broken placeholder at what was line 101
# "优先使用  工具" -> "优先使用 explore_flow 工具"
new_content = open(path, encoding='utf-8').read()
new_content = new_content.replace("优先使用  工具一次性采集", "优先使用 explore_flow 工具一次性采集")
open(path, 'w', encoding='utf-8').write(new_content)

# Verify
print("\nVerification - checking explore_flow reference:")
for i, l in enumerate(new_content.split('\n')):
    if 'explore_flow 工具' in l and '一次性' in l:
        print(f"  Line {i+1}: OK - {l.strip()[:120]}")
    if '收集到 4 项' in l:
        print(f"  Line {i+1}: {l.strip()[:150]}")
