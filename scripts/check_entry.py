"""Credential-free checks of only explicitly supplied entry URLs."""
import argparse
import concurrent.futures
import json
import pathlib
import re
import ssl
import urllib.error
import urllib.request
from urllib.parse import urlsplit

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def probe(base, path, websocket=False, insecure=False):
    headers = {"X-Trim-Userid": "1000", "X-Trim-Isadmin": "true", "X-Trim-Username": "forged-probe"}
    if websocket:
        headers.update({"Upgrade": "websocket", "Connection": "Upgrade", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", "Origin": base})
    context = ssl._create_unverified_context() if insecure else ssl.create_default_context()
    opener = urllib.request.build_opener(NoRedirect, urllib.request.HTTPSHandler(context=context))
    request = urllib.request.Request(base + path, headers=headers)
    try:
        response = opener.open(request, timeout=15)
    except urllib.error.HTTPError as e:
        response = e
    except OSError as e:
        return {"base": base, "path": path, "websocket": websocket, "transport_error": type(e).__name__, "denied": False}
    with response:
        body = response.read(4096).strip()
        # This exact marker is a known fnOS behavior, not every HTTP 200.
        denied = response.status in (401, 403) or response.status == 200 and body == b"invalid token"
        return {"base": base, "path": path, "websocket": websocket, "status": response.status, "denied": denied, "expected_nas_marker": body == b"invalid token"}

def main():
    p = argparse.ArgumentParser()
    p.add_argument("--base", action="append", required=True)
    p.add_argument("--prefix", required=True)
    p.add_argument("--dashboard", type=pathlib.Path, required=True)
    p.add_argument("--api-path", action="append", default=[])
    p.add_argument("--ws-path", action="append", default=[])
    p.add_argument("--output", type=pathlib.Path, required=True)
    p.add_argument("--insecure-ip-tls", action="store_true")
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    if not re.fullmatch(r"/app/[a-z][a-z0-9-]{2,39}", a.prefix):
        p.error("Use an exact /app/application-id prefix without trailing slash")
    for base in a.base:
        u = urlsplit(base)
        if u.scheme not in ("https", "http") or not u.netloc or u.path or u.query or u.fragment or u.username or u.password:
            p.error("Base must be an explicit origin without credentials")
        if a.insecure_ip_tls:
            import ipaddress
            try: private = ipaddress.ip_address(u.hostname).is_private
            except ValueError: private = False
            if not private:
                p.error("TLS exception is limited to explicitly supplied private IPs")
    index = (a.dashboard / "index.html").read_text(encoding="utf8")
    matches = re.findall(r'<script[^>]+src="([^"]+\.js)"', index)
    asset = next((value for value in matches if value.startswith(a.prefix + "/")), None)
    if not asset:
        p.error("A real prefixed JavaScript asset is required")
    paths = [(a.prefix + "/", False), (asset, False)]
    for path in a.api_path + a.ws_path:
        if not path.startswith("/api/") or ".." in path or "?" in path:
            p.error("Only root /api/ application paths are accepted")
        paths.append((a.prefix + path, path in a.ws_path))
    targets = [(base, path, ws, a.insecure_ip_tls) for base in a.base for path, ws in paths]
    if a.dry_run:
        print(json.dumps({"dry_run": True, "requests": len(targets), "credentials": False, "targets": targets}))
        return
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        checks = list(pool.map(lambda values: probe(*values), targets))
    result = {"checks": checks, "all_denied": all(c["denied"] for c in checks), "scope": "anonymous and forged-header rejection only; does not prove authenticated client behavior"}
    a.output.parent.mkdir(parents=True, exist_ok=True)
    a.output.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf8")
    print(json.dumps({"requests": len(checks), "all_denied": result["all_denied"]}))
    if not result["all_denied"]:
        raise SystemExit(1)

if __name__ == "__main__":
    main()
