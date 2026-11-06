#!/usr/bin/env python3
"""Publish an immutable APK build release using the authenticated gh CLI."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.parse import quote


TAG_PATTERN = re.compile(r"v\d+\.\d+\.\d+-build\.(\d+)\Z")


def gh(*args):
    return subprocess.run(
        ["gh", *args], check=True, text=True, stdout=subprocess.PIPE
    ).stdout


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def verify_artifact(directory, tag):
    apk = directory / f"sehat-{tag}.apk"
    checksum = directory / "SHA256SUMS.txt"
    lines = checksum.read_text().splitlines()
    expected = re.fullmatch(r"([0-9a-f]{64}) [ *](.+)", lines[0]) if len(lines) == 1 else None
    if not expected or expected.group(2) != apk.name or digest(apk) != expected.group(1):
        raise ValueError(f"Invalid checksum for {apk}")
    return [apk, checksum]


def highest_build(releases):
    numbers = []
    for release in releases:
        if release["draft"] or release["prerelease"]:
            continue
        candidates = [release["tag_name"]]
        candidates.extend(
            asset["name"][6:-4]
            for asset in release["assets"]
            if asset["name"].startswith("sehat-") and asset["name"].endswith(".apk")
        )
        numbers.extend(int(match.group(1)) for value in candidates if (match := TAG_PATTERN.fullmatch(value)))
    return max(numbers, default=-1)


def previous_downloads(repo, releases, current_tag):
    downloads = []
    for release in releases:
        if release["draft"] or release["prerelease"]:
            continue
        for asset in release["assets"]:
            name = asset["name"]
            if not name.startswith("sehat-") or not name.endswith(".apk"):
                continue
            tag = name[6:-4]
            match = TAG_PATTERN.fullmatch(tag)
            if match and tag != current_tag:
                url = f"https://github.com/{repo}/releases/download/{quote(release['tag_name'], safe='')}/{quote(name, safe='')}"
                downloads.append((int(match.group(1)), tag, url))
    downloads.sort(reverse=True)
    if not downloads:
        return ""
    return "\n\n### Previous APK downloads\n" + "\n".join(
        f"- [{tag}]({url})" for _, tag, url in downloads
    )


def publish(repo, sha, tag, signing, run_id, directory):
    match = TAG_PATTERN.fullmatch(tag)
    if not match or not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("Invalid build tag or commit SHA")
    if signing not in ("debug", "release") or not run_id.isdigit():
        raise ValueError("Invalid signing mode or workflow run ID")
    files = verify_artifact(directory, tag)
    refs = json.loads(gh("api", f"repos/{repo}/git/matching-refs/tags/{tag}"))
    ref = next((item for item in refs if item["ref"] == f"refs/tags/{tag}"), None)
    if ref is None:
        gh("api", f"repos/{repo}/git/refs", "-f", f"ref=refs/tags/{tag}", "-f", f"sha={sha}")
    elif ref["object"]["type"] != "commit" or ref["object"]["sha"] != sha:
        raise ValueError(f"Existing tag {tag} does not point directly to {sha}; refusing to change it")

    pages = json.loads(gh("api", "--paginate", "--slurp", f"repos/{repo}/releases?per_page=100"))
    releases = [release for page in pages for release in page]
    release = next((item for item in releases if item["tag_name"] == tag), None)
    if release is None:
        signing_note = (
            "**Debug-signed APK:** debug keys may differ between builds. Android may require "
            "uninstalling an existing installation signed with another key, which can remove local data."
            if signing == "debug"
            else "**Release-signed APK.** Android updates require the same signing key as the installed app."
        )
        notes = (
            f"{signing_note}\n\nCommit: `{sha}`  \n"
            f"[CI run](https://github.com/{repo}/actions/runs/{run_id}) · "
            f"[All releases and earlier downloads](https://github.com/{repo}/releases)\n\n"
            "Verify the APK against `SHA256SUMS.txt` before installing."
            + previous_downloads(repo, releases, tag)
        )
        gh("release", "create", tag, "--repo", repo, "--verify-tag", "--draft",
           "--title", f"Sehat {tag}", "--notes", notes)
        release = {"draft": True, "assets": []}

    assets = {asset["name"] for asset in release["assets"]}
    with tempfile.TemporaryDirectory() as temp:
        for file in files:
            if file.name in assets:
                gh("release", "download", tag, "--repo", repo, "--pattern", file.name, "--dir", temp)
                if digest(Path(temp) / file.name) != digest(file):
                    raise ValueError(f"Existing release asset {file.name} differs; refusing to overwrite it")
            else:
                gh("release", "upload", tag, str(file), "--repo", repo)

    if release["draft"]:
        gh("release", "edit", tag, "--repo", repo, "--draft=false", "--latest=false")
    if int(match.group(1)) >= highest_build(releases):
        gh("release", "edit", tag, "--repo", repo, "--latest")
    print(f"Published {tag}: https://github.com/{repo}/releases/tag/{tag}")


def main():
    try:
        publish(
            os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_SHA"],
            os.environ["BUILD_TAG"], os.environ["SIGNING"], os.environ["GITHUB_RUN_ID"],
            Path("dist"),
        )
    except (KeyError, OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"APK release failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
