#!/usr/bin/env python3
"""Collect Go and dependency license files for release archives."""

import json
from pathlib import Path
import re
import subprocess
import sys


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        item, end = decoder.raw_decode(text)
        yield item
        text = text[end:]


def main():
    notices = ["Go\n\n" + (Path(run("go", "env", "GOROOT")) / "LICENSE").read_text()]
    for module in sorted(objects(run("go", "list", "-m", "-json", "all")), key=lambda m: m["Path"]):
        if module.get("Main"):
            continue
        if not module.get("Dir"):
            module.update(json.loads(run("go", "mod", "download", "-json", f'{module["Path"]}@{module["Version"]}')))
        directory = Path(module["Dir"])
        licenses = sorted(p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE")))
        if not licenses:
            # Some modules declare their license and author only in the README.
            licenses = sorted(p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith("README") and re.search(r"^#+\s+licen[sc]e\b", p.read_text(), re.I | re.M))
        if not licenses:
            raise ValueError(f'no license file found for {module["Path"]} {module["Version"]}')
        section = f'{module["Path"]} {module["Version"]}\n\n'
        section += "\n\n".join(p.name + "\n\n" + p.read_text() for p in licenses)
        notices.append(section)
    print("\n\n" + ("\n\n" + "=" * 72 + "\n\n").join(notices))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        sys.exit(f"third-party-notices: {error}")
