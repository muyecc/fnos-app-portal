"""Build an external portal frontend; never patch an installed Octop backend.

Run --dry-run first. --source must be a disposable, verified upstream source
tree. All changes are recorded, and network URL checks fail on unhandled drift.
"""
import argparse
import hashlib
import json
import os
import pathlib
import re
import subprocess

HERE = pathlib.Path(__file__).resolve().parents[1] / "assets"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def transform(source, app_id="example-portal"):
    dash = source / "dashboard"
    changes = {}
    originals = {}

    def read(name):
        if name in changes:
            return changes[name]
        text = (dash / name).read_text(encoding="utf8")
        originals[name] = text
        return text

    def replace(name, old, new, count=1):
        text = read(name)
        if text.count(old) != count:
            raise ValueError(f"Upstream patch target drift: {name}: {old[:65]!r}")
        changes[name] = text.replace(old, new)

    # Replace root-anchored WebSocket builders, including chat, terminal,
    # desktop, browser and optional mobile. Keep the adaptation in one build.
    websocket_files = []
    for p in sorted((dash / "src").rglob("*.ts*")):
        if ".test." in p.name:
            continue
        name = p.relative_to(dash).as_posix()
        text = p.read_text(encoding="utf8")
        pat = r"(window\.location\.host\s*\})/api"
        if re.search(pat, text):
            rel = os.path.relpath(dash / "src/utils/publicUrl", p.parent).replace("\\", "/")
            if not rel.startswith("."):
                rel = "./" + rel
            originals[name] = text
            changes[name] = f'import {{ routerBasename as portalBase }} from "{rel}";\n' + re.sub(pat, r"\1${portalBase()}/api", text)
            websocket_files.append(name)
    if len(websocket_files) != 6:
        raise ValueError("Upstream WebSocket builders changed; review before adapting this version")

    # Make the shared API boundary idempotent for backend-generated file URLs.
    replace("src/api/config.ts", 'return `${base}${apiPrefix}${normalizedPath}`;',
            'if (base && normalizedPath.startsWith(`${base}/api/`)) return normalizedPath;\n'
            '  if (normalizedPath.startsWith("/api/")) return `${base}${normalizedPath}`;\n'
            '  return `${base}${apiPrefix}${normalizedPath}`;')
    # NAS apps share an origin. Never reuse/remove another application's
    # generic auth_token or remember-preference storage entries.
    replace("src/api/request.ts", 'const AUTH_TOKEN_KEY = "auth_token";',
            'const AUTH_TOKEN_KEY = "octop:portal:auth_token";')
    replace("src/api/request.ts", 'const AUTH_REMEMBER_PREF_KEY = "auth_remember_pref";',
            'const AUTH_REMEMBER_PREF_KEY = "octop:portal:auth_remember_pref";')
    auth_test="src/api/request.authToken.test.ts"
    text=read(auth_test)
    if '"auth_token"' not in text:
        raise ValueError("Auth storage test target drift")
    text=text.replace('"auth_token"','"octop:portal:auth_token"')
    text=text.replace('  it("defaults to a remembered',
        '  it("does not read or clear another same-origin app token", () => {\n'
        '    localStorage.setItem("auth_token", "other-app-token");\n'
        '    expect(mod.getAuthToken()).toBe("");\n'
        '    mod.setAuthToken("own-token"); mod.clearAuthToken();\n'
        '    expect(localStorage.getItem("auth_token")).toBe("other-app-token");\n'
        '  });\n\n  it("defaults to a remembered')
    changes[auth_test]=text

    # Preserve the existing deployed image-provider compatibility fix.
    replace("src/pages/Settings/MediaGeneration/index.tsx", 'video_enabled: true,\n        image_model: preset.image_models[0]',
            'video_enabled: preset.video_models.length > 0,\n        image_model: preset.image_models[0]')

    md = "src/components/Markdown/index.tsx"
    text = read(md)
    changes[md] = ('import { useChatFilePreview } from "../../pages/Chat/ChatFilePreviewContext";\n'
                   'import { workspaceLinkTarget, markdownUrlTransform } from "../../portalWorkspaceLinks";\n'
                   'import { portalUrl } from "../../portalPaths";\n' + text)
    replace(md, '  const isDark = useIsDark();', '  const filePreview = useChatFilePreview();\n  const isDark = useIsDark();')
    replace(md, '        remarkPlugins={remarkPlugins}',
            '        urlTransform={(url, key) => markdownUrlTransform(url, key, !!filePreview)}\n        remarkPlugins={remarkPlugins}')
    replace(md, '            if (isAuthDownloadHref(href)) {',
            '            const target = filePreview && workspaceLinkTarget(href);\n'
            '            if (target) return <a {...props} href={href} data-octop-workspace-link="portal" '
            'onClick={(event) => { event.preventDefault(); filePreview!.openFilePreview(target.path, target.agentId); }}>{children}</a>;\n'
            '            if (isAuthDownloadHref(href)) {')
    replace(md, '    return src;\n  }\n\n  let filePath', '    return portalUrl(src);\n  }\n\n  let filePath')
    replace(md, 'return `/api/workspace/media?path=${encodeURIComponent(relPath)}`;',
            'return portalUrl(`/api/workspace/media?path=${encodeURIComponent(relPath)}`);')

    # Disable PWA/offline registration in this authenticated NAS embedding. A
    # cached shell must not give the impression that NAS authentication passed.
    replace("src/main.tsx", 'import "./pwa-prompt";', 'import "./portalLease";')
    replace("src/main.tsx", '  void import("./sw-register").then(({ registerSW }) => registerSW());',
            '  // Portal access always goes through the NAS gateway; no offline worker.')

    # The fnOS app WebView already sits below native top chrome. Its reported
    # safe-area top must not be added a second time to the Octop mobile header
    # or navigation drawer. Keep bottom/left/right and the upstream PWA intact.
    top = 'var(--safe-area-top, 0px)'
    replace("src/styles/layout.css", '--safe-area-top: env(safe-area-inset-top, 0px);',
            '--safe-area-top: 0px; /* fnOS host owns the top system inset */')
    replace("src/layouts/Header.tsx", 'const safeTop = "env(safe-area-inset-top, 0px)";',
            f'const safeTop = "{top}";')
    replace("src/layouts/Header.tsx", '  // iOS PWA (`apple-mobile-web-app-status-bar-style: black-translucent`) draws\n'
            '  // under the status bar; pad the chrome so controls stay tappable (#664).',
            '  // fnOS provides native top chrome; use the portal-owned inset once.')
    replace("src/layouts/Sidebar.tsx", 'env(safe-area-inset-top, 0px)', top, count=2)
    replace("src/layouts/Header.test.tsx", 'pads mobile chrome for iOS PWA safe-area inset',
            'uses the host-owned top inset without adding the PWA status bar')
    replace("src/layouts/Header.test.tsx", 'toContain("safe-area-inset-top")',
            'toContain("var(--safe-area-top, 0px)")', count=2)
    changes["src/portalLayout.test.ts"] = (HERE / "octop-frontend/portalLayout.test.ts").read_text(encoding="utf8")

    # Same-origin NAS apps must never clear each other's Service Workers or
    # caches. The portal needs no worker; keep the upstream export boundary.
    sw = "src/sw-register.ts"
    read(sw)
    changes[sw] = ('export function peekSystemNotifyRegistration(): ServiceWorkerRegistration | null { return null; }\n'
                   'export async function applyUpdate(): Promise<void> { window.location.reload(); }\n'
                   'export async function registerProductionSW(): Promise<void> {}\n'
                   'export async function registerSW(): Promise<void> {}\n')
    stale = "src/utils/reloadOnStaleChunk.ts"
    text = read(stale)
    pat = r'(export async function bustServiceWorkerAndReload\(\): Promise<void> \{).*?(\n  window.location.reload\(\);\n\})'
    if len(re.findall(pat, text, re.S)) != 1:
        raise ValueError("Stale-chunk cleanup target drift")
    changes[stale] = re.sub(pat, r'\1\n  // Portal has no offline caches; leave the NAS and other applications intact.\2', text, flags=re.S)
    pwa = "src/pages/PwaDebug/index.tsx"
    text = read(pwa)
    pat = r'  const handleResetPwa = async \(\) => \{.*?\n  \};'
    if len(re.findall(pat, text, re.S)) != 1:
        raise ValueError("PWA reset target drift")
    changes[pwa] = re.sub(pat, '  const handleResetPwa = async () => {\n    location.reload();\n  };', text, flags=re.S)
    replace("src/hooks/useDashboardPushToast.tsx", 'if ("serviceWorker" in navigator) {',
            'if (peekSystemNotifyRegistration() && "serviceWorker" in navigator) {')

    # Link matching must accept both backend root URLs and portal-prefixed URLs.
    link = (HERE / "octop-frontend/workspace-links.mjs").read_text(encoding="utf8")
    link = 'import { routerBasename } from "./utils/publicUrl";\n' + link
    link = link.replace('let path = href.trim(), agentId;',
                        'let path = href.trim(), agentId;\n  const base = routerBasename();\n'
                        '  if (base && path.startsWith(base + "/api/")) path = path.slice(base.length);')
    link = link.replace('    const match = url.pathname.match(',
                        '    if (base && url.pathname.startsWith(base + "/api/")) url.pathname = url.pathname.slice(base.length);\n    const match = url.pathname.match(')
    changes["src/portalWorkspaceLinks.js"] = "// @ts-nocheck\n" + link
    changes["src/portalWorkspaceLinks.d.ts"] = (
        'export function workspaceLinkTarget(href: string | null | undefined, origin?: string): {path: string; agentId?: string} | null;\n'
        'export function markdownUrlTransform(url: string, key: string, previewAvailable: boolean): string;\n')
    for n in ["portalLease.ts", "portalPaths.ts"]:
        changes["src/"+n] = (HERE / "octop-frontend" / n).read_text(encoding="utf8")
    changes["src/portalPaths.test.ts"] = (HERE / "octop-frontend/portalPaths.test.ts").read_text(encoding="utf8")

    # Any new direct root fetch/WS/URL constructor needs review before release.
    unhandled = []
    for p in (dash / "src").rglob("*.ts*"):
        if ".test." in p.name:
            continue
        name = p.relative_to(dash).as_posix()
        text = changes.get(name, p.read_text(encoding="utf8"))
        if re.search(r'''(?:fetch|new\s+(?:WebSocket|URL))\(\s*["'`]/api(?:/|["'`])''', text):
            unhandled.append(name)
    if unhandled:
        raise ValueError("Unadapted root network URLs: " + ", ".join(unhandled))
    changes = {name: text.replace("octop-portal", app_id).replace("octop:portal:", app_id + ":") for name, text in changes.items()}
    manifest = {"schema": 1, "websocket_files": websocket_files, "files": {
        k: {"original_sha256": digest(originals[k].encode()) if k in originals else None,
            "patched_sha256": digest(v.encode())} for k,v in changes.items()}}
    return changes, manifest


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--app-id", default="example-portal")
    p.add_argument("--source", type=pathlib.Path, required=True)
    p.add_argument("--output", type=pathlib.Path, required=True)
    p.add_argument("--version", required=True)
    p.add_argument("--commit", required=True)
    p.add_argument("--dry-run", action="store_true")
    a = p.parse_args()
    if not re.fullmatch(r"[a-z][a-z0-9-]{2,39}", a.app_id):
        p.error("invalid application id")
    if not re.fullmatch(r"[0-9a-f]{40}", a.commit):
        p.error("a pinned upstream commit is required")
    a.source=a.source.resolve();a.output=a.output.resolve()
    provenance=json.loads((a.source/".portal-upstream.json").read_text(encoding="utf8"))
    if provenance["commit"] != a.commit or provenance["version"] != a.version:
        raise ValueError("Pinned source provenance mismatch")
    changes,manifest=transform(a.source,a.app_id)
    manifest.update({"upstream_version":a.version,"upstream_commit":a.commit,"upstream_archive_sha256":provenance["archive_sha256"],"prefix":f"/app/{a.app_id}/"})
    if not a.dry_run:
        if a.output.exists():
            raise ValueError("Use a new release output directory")
        for name,text in changes.items():
            path=a.source/"dashboard"/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_text(text,encoding="utf8",newline="\n")
        env=dict(os.environ,VITE_BASE_PATH=f"/app/{a.app_id}/",BASE_URL="",NODE_OPTIONS="--max-old-space-size=8192")
        command="npm.cmd" if os.name=="nt" else "npm"
        subprocess.run([command,"exec","--","tsc","-b"],cwd=a.source/"dashboard",env=env,check=True)
        subprocess.run([command,"exec","--","vitest","run","src/portalLayout.test.ts","src/layouts/Header.test.tsx","src/portalPaths.test.ts","src/utils/publicUrl.test.ts","src/utils/downloadLink.test.tsx","src/api/request.authToken.test.ts"],cwd=a.source/"dashboard",env=env,check=True)
        subprocess.run([command,"run","build:docker","--","--outDir",str(a.output)],cwd=a.source/"dashboard",env=env,check=True)
        # PWA packages are not served by this portal. No cached authenticated UI.
        for path in a.output.glob("*sw*.js"):
            path.unlink()
        index=a.output/"index.html"
        text=index.read_text(encoding="utf8")
        text=re.sub(r'<link[^>]+rel="manifest"[^>]*>',"",text)
        index.write_text(text,encoding="utf8")
        manifest["assets"]={str(p.relative_to(a.output)).replace("\\","/"):digest(p.read_bytes()) for p in a.output.rglob("*") if p.is_file()}
        (a.output/"portal-build.json").write_text(json.dumps(manifest,indent=2),encoding="utf8")
    print(json.dumps({"dry_run":a.dry_run,"version":a.version,"changed_files":len(changes),"websocket_files":manifest["websocket_files"]},ensure_ascii=False))


if __name__=="__main__":
    main()
