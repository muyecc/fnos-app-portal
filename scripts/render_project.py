"""Render a new local project; never connect to or mutate a NAS."""
import argparse
import ipaddress
import json
import pathlib
import re
import shutil
import hashlib
from urllib.parse import urlsplit

ROOT = pathlib.Path(__file__).resolve().parents[1]

def validate(plan, dashboard):
    for field in ['display_name', 'package_version']:
        if not isinstance(plan.get(field), str) or any(c in plan[field] for c in '\r\n'):
            raise ValueError('Manifest values must be single-line strings')
    app = plan["app_id"]
    if not re.fullmatch(r"[a-z][a-z0-9-]{2,39}", app):
        raise ValueError("Invalid app_id")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:[-.a-zA-Z0-9]*)?", plan["package_version"]):
        raise ValueError("Use a pinned package version")
    if not re.fullmatch(r"[a-zA-Z0-9_.-]+", plan["docker_network"]):
        raise ValueError("Invalid Docker network")
    if plan.get("architecture") != "amd64":
        raise ValueError("The supplied FPK baseline is x86; review other architectures separately")
    if plan.get("peer_uid_verified") is not True or not plan.get("gateway_peer_uids"):
        raise ValueError("Verify the real Unix peer UID before rendering")
    if any(type(uid) is not int or not 0 <= uid < 2**32 for uid in plan["gateway_peer_uids"]):
        raise ValueError("Invalid peer UID")
    if not plan.get("admins_only", True) and not plan.get("allowed_user_ids"):
        raise ValueError("Explicit user policy required")
    hosts = plan["public_hosts"]
    if not hosts or any(not h or h != h.lower() or any(c in h for c in "/*?#@ \t\r\n") for h in hosts):
        raise ValueError("Exact lowercase hosts required")
    origins = plan["allowed_origins"]
    if not origins:
        raise ValueError("Explicit browser origins required")
    exceptions = plan.get("http_cookie_ips", [])
    for host in exceptions:
        ip = ipaddress.ip_address(host)
        private_ranges = [ipaddress.ip_network(value) for value in ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16']]
        if ip.version != 4 or not any(ip in network for network in private_ranges):
            raise ValueError("This renderer supports explicit private IPv4 HTTP exceptions only")
    for origin in origins:
        u = urlsplit(origin)
        if u.scheme not in ("http", "https") or u.netloc not in hosts or u.username or u.password or u.path or u.query or u.fragment:
            raise ValueError("Invalid origin or missing exact host")
        if origin != origin.lower():
            raise ValueError("Use lowercase origins")
        if u.scheme == "http" and u.hostname not in exceptions:
            raise ValueError("HTTP requires an explicit IP cookie exception")
    for ip in exceptions:
        if not any(urlsplit(o).scheme == "http" and urlsplit(o).hostname == ip for o in origins):
            raise ValueError("HTTP cookie IP lacks its explicit origin")
    upstream = urlsplit(plan["upstream"])
    if upstream.scheme != "http" or not upstream.hostname or upstream.username or upstream.password or upstream.path or upstream.query or upstream.fragment:
        raise ValueError("Use an internal HTTP service origin, without credentials or a path")
    try:
        ipaddress.ip_address(upstream.hostname)
    except ValueError:
        pass
    else:
        raise ValueError("Use a Docker service name rather than a changeable container IP")
    prefix = "/app/" + app + "/"
    manifest = json.loads((dashboard / "portal-build.json").read_text(encoding="utf8"))
    if manifest.get("prefix") != prefix or manifest.get("upstream_version") != plan["upstream_version"]:
        raise ValueError("Frontend prefix/version mismatch")
    assets = manifest.get("assets")
    if not isinstance(assets, dict) or "index.html" not in assets:
        raise ValueError("A complete frontend manifest is required")
    for name, expected in assets.items():
        pure = pathlib.PurePosixPath(name)
        if pure.is_absolute() or ".." in pure.parts or "\\" in name or ":" in name or not name:
            raise ValueError("Unsafe frontend path")
        item = dashboard / name
        if item.is_symlink() or not item.resolve().is_relative_to(dashboard.resolve()) or not item.is_file():
            raise ValueError("Unsafe or missing frontend asset")
        if hashlib.sha256(item.read_bytes()).hexdigest() != expected:
            raise ValueError("Frontend hash mismatch: " + name)
    return manifest

def render(plan, dashboard, output, dry_run=False):
    if output.exists():
        raise ValueError("Choose a new output directory")
    manifest = validate(plan, dashboard)
    app = plan["app_id"]
    if dry_run:
        return {"dry_run": True, "app_id": app, "assets": len(manifest["assets"]), "nas_changes": False}
    shutil.copytree(ROOT / "assets/portal", output)
    for name in ["fpk/manifest", "fpk/app/ui/config", "fpk/config/privilege", "fpk/config/resource", "fpk/app/docker/docker-compose.yaml"]:
        p = output / name
        text = p.read_text(encoding="utf8").replace("example-portal", app)
        if name == "fpk/manifest":
            text = re.sub(r"(?m)^version=.*$", "version=" + plan["package_version"], text)
            text = re.sub(r"(?m)^display_name=.*$", "display_name=" + plan["display_name"], text)
        elif name.endswith("ui/config"):
            obj = json.loads(text)
            obj[".url"][app + ".main"]["title"] = plan["display_name"]
            text = json.dumps(obj, ensure_ascii=False, indent=2) + "\n"
        elif name.endswith("docker-compose.yaml"):
            text = text.replace(app + "-local:1.0.1", app + "-local:" + plan["package_version"])
            text = text.replace("name: example_default", "name: " + plan["docker_network"])
        p.write_text(text, encoding="utf8", newline="\n")
    cfg = {
        "prefix": "/app/" + app, "socket": "/app/target/app.sock",
        "upstream": plan["upstream"], "static_dir": "/app/target/dashboard",
        "frontend_version": plan["upstream_version"] + "-portal1",
        "gateway_peer_uids": plan["gateway_peer_uids"],
        "allowed_user_ids": plan.get("allowed_user_ids", []), "admins_only": plan.get("admins_only", True),
        "public_hosts": plan["public_hosts"], "allowed_origins": plan["allowed_origins"],
        "http_cookie_ips": plan.get("http_cookie_ips", []), "secure_cookie": True, "lease_seconds": 30,
    }
    (output / "fpk/app/config.json").write_text(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n", encoding="utf8")
    target = output / "fpk/app/dashboard"
    for name in [*manifest["assets"], "portal-build.json"]:
        p = target / name
        p.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(dashboard / name, p)
    return {"rendered": True, "app_id": app, "assets": len(manifest["assets"]), "nas_changes": False}

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--plan", type=pathlib.Path, required=True)
    p.add_argument("--dashboard", type=pathlib.Path, required=True)
    p.add_argument("--output", type=pathlib.Path, required=True)
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    plan = json.loads(a.plan.read_text(encoding="utf8"))
    if any("\n" in str(plan.get(k, "")) or "\r" in str(plan.get(k, "")) for k in ["display_name", "package_version"]):
        p.error("Manifest values cannot contain line breaks")
    print(json.dumps(render(plan, a.dashboard.resolve(), a.output.resolve(), a.dry_run), ensure_ascii=False, indent=2))

if __name__ == "__main__":
    main()
