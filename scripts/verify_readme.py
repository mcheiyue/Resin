import re
import sys
from pathlib import Path


def has_cjk(text: str) -> bool:
    return bool(re.search(r"[\u4e00-\u9fff]", text))


def main() -> int:
    target = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("README.md")
    if not target.exists():
        print("README_LANGUAGE=UNKNOWN")
        print("FORK_POSITIONING=MISSING")
        print("UPSTREAM_DISTINCTION=MISSING")
        print("README_NOT_UPSTREAM_CLONE=FALSE")
        return 1

    content = target.read_text(encoding="utf-8", errors="ignore")
    lines = [line.strip() for line in content.splitlines() if line.strip()]

    zh_first = any(has_cjk(line) for line in lines[:5])

    lower_content = content.lower()
    fork_positioning = (
        "windows-first" in lower_content or "windows first" in lower_content
    ) and ("桌面化" in content and "fork" in lower_content)

    upstream_distinction = "不是上游官方桌面客户端" in content

    not_upstream_clone = (
        "high-performance intelligent proxy pool gateway" not in lower_content
        and "[english](readme.md)" not in lower_content
        and "readme.zh-cn.md" not in lower_content
    )

    print(f"README_LANGUAGE={'ZH_FIRST' if zh_first else 'UNKNOWN'}")
    print(f"FORK_POSITIONING={'OK' if fork_positioning else 'MISSING'}")
    print(f"UPSTREAM_DISTINCTION={'OK' if upstream_distinction else 'MISSING'}")
    print(f"README_NOT_UPSTREAM_CLONE={'TRUE' if not_upstream_clone else 'FALSE'}")

    return (
        0
        if all([zh_first, fork_positioning, upstream_distinction, not_upstream_clone])
        else 1
    )


if __name__ == "__main__":
    raise SystemExit(main())
