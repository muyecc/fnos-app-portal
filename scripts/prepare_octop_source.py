"""Extract a pinned upstream archive into a new disposable source tree."""
import argparse
import hashlib
import json
import pathlib
import re
import zipfile


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--archive", type=pathlib.Path, required=True)
    p.add_argument("--sha256", required=True)
    p.add_argument("--commit", required=True)
    p.add_argument("--version", required=True)
    p.add_argument("--output", type=pathlib.Path, required=True)
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", a.commit):
        p.error("Pin an exact upstream commit")
    actual = hashlib.sha256(a.archive.read_bytes()).hexdigest()
    if actual != a.sha256:
        raise ValueError("Upstream archive checksum mismatch")
    if a.output.exists():
        raise ValueError("Output must be a new disposable directory")
    root = f"Octop-{a.commit}"
    with zipfile.ZipFile(a.archive) as z:
        checked = []
        for item in z.infolist():
            path = pathlib.PurePosixPath(item.filename)
            if not path.parts or path.is_absolute() or ".." in path.parts or "\\" in item.filename or ":" in item.filename or path.parts[0] != root:
                raise ValueError("Unexpected upstream archive path")
            if len(path.parts) > 1 and not item.is_dir():
                checked.append((item, pathlib.Path(*path.parts[1:])))
        if not a.dry_run:
            for item, relative in checked:
                dest = a.output / relative
                dest.parent.mkdir(parents=True, exist_ok=True)
                dest.write_bytes(z.read(item))
            (a.output / ".portal-upstream.json").write_text(json.dumps({"commit": a.commit, "version": a.version, "archive_sha256": actual}), encoding="utf8")
    print(json.dumps({"dry_run": a.dry_run, "files": len(checked), "commit": a.commit, "sha256": actual}))


if __name__ == "__main__":
    main()
