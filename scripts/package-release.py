#!/usr/bin/env python3
"""Package already-built binaries and generate Homebrew/Nix release metadata."""

import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = (
    ("darwin", "arm64", "aarch64-darwin"),
    ("darwin", "amd64", "x86_64-darwin"),
    ("linux", "amd64", "x86_64-linux"),
    ("linux", "arm64", "aarch64-linux"),
    ("linux", "arm", None),
    ("windows", "amd64", None),
    ("windows", "arm64", None),
)


def archive(path, entries, windows):
    with path.open("xb") as output:
        if windows:
            with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as packed:
                for name, data, mode in entries:
                    member = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                    member.create_system = 3
                    member.external_attr = (0o100000 | mode) << 16
                    member.compress_type = zipfile.ZIP_DEFLATED
                    packed.writestr(member, data)
        else:
            with gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w") as packed:
                    for name, data, mode in entries:
                        member = tarfile.TarInfo(name)
                        member.size, member.mode, member.mtime = len(data), mode, 0
                        packed.addfile(member, io.BytesIO(data))


def package(version, repository, binaries, notices, output):
    if not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", version):
        raise ValueError("version must be a stable version without v, such as 1.0.0")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("repository must be in owner/repo form")
    payloads = []
    notice_text = notices.read_bytes()
    if not notice_text.strip():
        raise ValueError("third-party notices are empty")
    # Check every input before creating a release directory.
    for os_name, arch, system in TARGETS:
        extension = ".exe" if os_name == "windows" else ""
        binary = binaries / f"evado_{os_name}_{arch}{extension}"
        data = binary.read_bytes()
        if not data:
            raise ValueError(f"empty binary: {binary}")
        payloads.append((os_name, arch, system, extension, data))
    output.mkdir(parents=True, exist_ok=False)
    assets, hashes, replacements = {}, {}, {
        "VERSION": version,
        "RELEASE_REPOSITORY": repository,
    }
    for os_name, arch, system, extension, data in payloads:
        suffix = ".zip" if extension else ".tar.gz"
        name = f"evado_{version}_{os_name}_{arch}{suffix}"
        entries = [(f"evado{extension}", data, 0o755), ("THIRD_PARTY_NOTICES.txt", notice_text, 0o644)]
        if (ROOT / "LICENSE").is_file():
            entries.append(("LICENSE", (ROOT / "LICENSE").read_bytes(), 0o644))
        archive(output / name, entries, bool(extension))
        digest = hashlib.sha256((output / name).read_bytes()).hexdigest()
        hashes[name] = digest
        if system:
            assets[system] = {
                "url": f"https://github.com/{repository}/releases/download/v{version}/{name}",
                "sha256": digest,
            }
            replacements[f"{os_name}_{arch}_SHA256".upper()] = digest
    manifest = {"version": version, "assets": assets}
    (output / "release.json").write_text(json.dumps(manifest, indent=2) + "\n")
    formula = (ROOT / "packaging/homebrew/evado.rb.in").read_text()
    for key, value in replacements.items():
        formula = formula.replace(f"@{key}@", value)
    if re.search(r"@[A-Z0-9_]+@", formula):
        raise ValueError("unresolved Homebrew formula token")
    (output / "evado.rb").write_text(formula)
    for name in ("release.json", "evado.rb"):
        hashes[name] = hashlib.sha256((output / name).read_bytes()).hexdigest()
    (output / "SHA256SUMS").write_text("".join(f"{hashes[name]}  {name}\n" for name in sorted(hashes)))
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version without v, for example 1.0.0")
    parser.add_argument("--repository", default="nerdlabz/evado-tui")
    parser.add_argument("--binaries", type=Path, default=ROOT / "dist")
    parser.add_argument("--notices", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="new directory, never overwritten")
    args = parser.parse_args()
    try:
        package(args.version, args.repository, args.binaries, args.notices, args.output)
    except (OSError, ValueError) as error:
        parser.exit(1, f"package-release: {error}\n")
    print(f"Release archives, checksums, evado.rb and release.json written to {args.output}")


if __name__ == "__main__":
    main()
