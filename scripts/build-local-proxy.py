#!/usr/bin/env python3

import argparse
import datetime
import json
import pathlib
import re
import subprocess
import zipfile

VERSION_RE = re.compile(r"^v\d+\.\d+\.\d+$")


def module_path(root: pathlib.Path) -> str:
    for line in (root / "go.mod").read_text().splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise SystemExit(f"module directive not found in {root / 'go.mod'}")


def repository_files(root: pathlib.Path) -> list[pathlib.Path]:
    git_root = pathlib.Path(
        subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "--show-toplevel"],
            text=True,
        ).strip()
    )
    root_from_git = root.relative_to(git_root)
    output = subprocess.check_output(
        [
            "git",
            "-C",
            str(git_root),
            "ls-files",
            "--cached",
            "--others",
            "--exclude-standard",
            "-z",
            "--",
            str(root_from_git) if root_from_git.parts else ".",
        ]
    )
    files: list[pathlib.Path] = []
    for raw_path in output.split(b"\0"):
        if not raw_path:
            continue
        source = git_root / pathlib.Path(raw_path.decode())
        path = source.relative_to(root)
        if path.parts and path.parts[0] in {".release-deps", ".release-proxy"}:
            continue
        if source.is_symlink():
            raise SystemExit(f"refusing to package symbolic link: {path}")
        if source.is_file():
            files.append(path)

    nested_modules = {
        path.parent
        for path in files
        if path.name == "go.mod" and path.parent != pathlib.Path(".")
    }
    return sorted(
        path
        for path in files
        if not any(nested in path.parents for nested in nested_modules)
    )


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Build an unpublished Go module version in file-proxy format."
    )
    parser.add_argument("output", type=pathlib.Path)
    parser.add_argument("version", nargs="?", default="v0.5.0")
    parser.add_argument(
        "--module-root",
        type=pathlib.Path,
        default=pathlib.Path(__file__).resolve().parents[1],
    )
    args = parser.parse_args()

    if not VERSION_RE.fullmatch(args.version):
        raise SystemExit(f"version must be vMAJOR.MINOR.PATCH: {args.version}")

    root = args.module_root.resolve()
    module = module_path(root)
    output = args.output.resolve()
    version_dir = output / module / "@v"
    version_dir.mkdir(parents=True, exist_ok=True)

    module_file = (root / "go.mod").read_bytes()
    (version_dir / f"{args.version}.mod").write_bytes(module_file)
    (version_dir / "list").write_text(f"{args.version}\n")
    (version_dir / f"{args.version}.info").write_text(
        json.dumps(
            {
                "Version": args.version,
                "Time": datetime.datetime.now(datetime.timezone.utc)
                .replace(microsecond=0)
                .isoformat()
                .replace("+00:00", "Z"),
            }
        )
        + "\n"
    )

    prefix = f"{module}@{args.version}/"
    with zipfile.ZipFile(
        version_dir / f"{args.version}.zip",
        "w",
        compression=zipfile.ZIP_DEFLATED,
    ) as archive:
        for relative_path in repository_files(root):
            archive.write(root / relative_path, prefix + relative_path.as_posix())

    print(output.as_uri())


if __name__ == "__main__":
    main()
