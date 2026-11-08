"""Test and build only a rendered local gateway project."""
import argparse
import json
import os
import pathlib
import re
import subprocess
import sys

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--project", type=pathlib.Path, required=True)
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    project = a.project.resolve()
    manifest = (project / "fpk/manifest").read_text(encoding="utf8")
    app = re.search(r"(?m)^appname=(.+)$", manifest).group(1)
    version = re.search(r"(?m)^version=(.+)$", manifest).group(1)
    commands = [["go", "test", "-count=1", "./..."], ["go", "vet", "./..."], ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", "dist/portal", "./cmd/portal"]]
    if a.dry_run:
        print(json.dumps({"dry_run": True, "commands": commands, "build_target": "linux/amd64", "image_tag": app + "-local:" + version, "nas_changes": False}))
        return
    hostenv = dict(os.environ)
    for key in ["GOOS", "GOARCH"]:
        hostenv.pop(key, None)
    for cmd in commands[:2]:
        subprocess.run(cmd, cwd=project, env=hostenv, check=True)
    (project / "dist").mkdir(exist_ok=True)
    subprocess.run(commands[2], cwd=project, env=dict(hostenv, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0"), check=True)
    print(json.dumps({"built": True, "binary": str(project / "dist/portal"), "image_tag": app + "-local:" + version, "linux_runtime_tested": sys.platform == "linux"}))

if __name__ == "__main__":
    main()
