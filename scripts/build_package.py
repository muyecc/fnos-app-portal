"""Stage and package an existing rendered project; no live installation."""
import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--project", type=pathlib.Path, required=True)
    p.add_argument("--output", type=pathlib.Path, required=True)
    p.add_argument("--fnpack", required=True)
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    project = a.project.resolve()
    output = a.output.resolve()
    if output.exists():
        raise ValueError("Use a new staging directory")
    dashboard = project / "fpk/app/dashboard"
    cfg = json.loads((project / "fpk/app/config.json").read_text(encoding="utf8"))
    manifest = json.loads((dashboard / "portal-build.json").read_text(encoding="utf8"))
    if manifest["prefix"].rstrip("/") != cfg["prefix"]:
        raise ValueError("Frontend and gateway prefixes differ")
    if not cfg["frontend_version"].startswith(manifest["upstream_version"] + "-"):
        raise ValueError("Frontend and gateway versions differ")
    for name, wanted in manifest["assets"].items():
        pure = pathlib.PurePosixPath(name)
        item = dashboard / name
        if pure.is_absolute() or ".." in pure.parts or "\\" in name or ":" in name or not item.resolve().is_relative_to(dashboard):
            raise ValueError("Unsafe frontend asset")
        if hashlib.sha256(item.read_bytes()).hexdigest() != wanted:
            raise ValueError("Frontend changed after rendering")
    tool = shutil.which(a.fnpack) or str(pathlib.Path(a.fnpack).resolve())
    if not pathlib.Path(tool).is_file():
        raise ValueError("Provide the official fnpack executable")
    if a.dry_run:
        print(json.dumps({"dry_run": True, "assets": len(manifest["assets"]), "command": [tool, "build", "--directory", str(output)], "nas_changes": False}))
        return
    shutil.copytree(project / "fpk", output)
    subprocess.run([tool, "build", "--directory", str(output)], cwd=output.parent, check=True)
    print(json.dumps({"staged": str(output), "installed": False}))

if __name__ == "__main__":
    main()
