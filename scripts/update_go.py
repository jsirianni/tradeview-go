"""Update go.mod to the newest stable Go release without changing dependencies."""

import json
import os
from pathlib import Path
import re
from urllib.request import urlopen


RELEASES_URL = "https://go.dev/dl/?mode=json"
GO_DIRECTIVE = re.compile(rb"^go[ \t]+(\d+\.\d+(?:\.\d+)?)(?=[ \t\r\n]|$)", re.M)


def version_tuple(version):
    parts = tuple(int(part) for part in version.split("."))
    return parts if len(parts) == 3 else (*parts, 0)


def update_module(content, releases):
    directives = list(GO_DIRECTIVE.finditer(content))
    if len(directives) != 1:
        raise ValueError("Expected exactly one stable Go directive in go.mod")
    if not isinstance(releases, list):
        raise ValueError("Expected a list from the Go release feed")
    versions = []
    for release in releases:
        version = release.get("version", "")
        if release.get("stable") is True and re.fullmatch(r"go\d+\.\d+\.\d+", version):
            versions.append(version[2:])
    if not versions:
        raise ValueError("The Go release feed contains no stable releases")
    latest = max(versions, key=version_tuple)
    directive = directives[0]
    current = directive.group(1).decode("ascii")
    if version_tuple(latest) <= version_tuple(current):
        return content, current
    start, end = directive.span(1)
    return content[:start] + latest.encode("ascii") + content[end:], latest


def main():
    module = Path("go.mod")
    original = module.read_bytes()
    with urlopen(RELEASES_URL, timeout=30) as response:
        updated, version = update_module(original, json.load(response))
    if updated != original:
        module.write_bytes(updated)
        print(f"Updated go.mod to Go {version}")
    else:
        print(f"Go {version} is already current; no changes")
    if output := os.environ.get("GITHUB_OUTPUT"):
        with open(output, "a", encoding="utf-8") as stream:
            stream.write(f"version={version}\n")


if __name__ == "__main__":
    main()
