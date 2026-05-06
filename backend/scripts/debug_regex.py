import sys, re
sys.path.insert(0, r"D:\AutoTestingLearingProject\AI_Web_Testing\backend")
from app.locators.semantic import _PARENT_TEXT_RE

pattern = _PARENT_TEXT_RE.pattern
print("Pattern:")
for i, ch in enumerate(pattern):
    print(f"  [{i}] U+{ord(ch):04X} {ch!r}")
print()

tests = [
    "Blue Top 附近的 Add to cart",
    "Fancy Green Top 附近的 Add to cart",
    "Blue Top 附近的 Rs. 500",
    "Fancy Green Top 的数量",
]
for s in tests:
    m = _PARENT_TEXT_RE.match(s)
    if m:
        print(f"OK: parent={m.group('parent')!r} child={m.group('child')!r}")
    else:
        # Manual match test
        print(f"FAIL: {s!r}")
        for part in ["\\u9644\\u8fd1\\u7684", "\\u7684\\u6570\\u91cf"]:
            if part in s:
                print(f"  Found {part}")
