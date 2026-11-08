// Adapt local file links to Octop's existing chat preview context.
// File access remains subject to the backend's workspace authorization.
const safeProtocol = /^(https?|ircs?|mailto|xmpp)$/i;

function defaultUrlTransform(value) {
  const colon = value.indexOf(":"), question = value.indexOf("?");
  const hash = value.indexOf("#"), slash = value.indexOf("/");
  return colon === -1 || (slash !== -1 && colon > slash) ||
    (question !== -1 && colon > question) || (hash !== -1 && colon > hash) ||
    safeProtocol.test(value.slice(0, colon)) ? value : "";
}

export function workspaceLinkTarget(href, origin = globalThis.location?.origin) {
  if (typeof href !== "string") return null;
  let path = href.trim(), agentId;
  if (!path || /[\u0000-\u001f\u007f]/.test(path) || path.startsWith("#") || path.startsWith("//")) return null;

  // Only recognize this app's authenticated workspace API, never external URLs.
  if (/^https?:/i.test(path) || path.startsWith("/api/")) {
    let url;
    try { url = new URL(path, origin); } catch { return null; }
    if (!origin || url.origin !== origin) return null;
    const match = url.pathname.match(/^\/api\/agents\/([^/]+)\/workspace\/(?:file|download|media)$/);
    if (!match && !/^\/api\/workspace\/(?:file|download-file|media)$/.test(url.pathname)) return null;
    path = url.searchParams.get("path") || "";
    if (match) { try { agentId = decodeURIComponent(match[1]); } catch { return null; } }
  } else if (/^file:/i.test(path)) {
    let url;
    try { url = new URL(path); } catch { return null; }
    if (url.hostname && url.hostname !== "localhost") return null;
    path = url.pathname;
  } else if (/^sandbox:/i.test(path)) {
    path = path.slice(8);
  } else if (/^[a-z][a-z0-9+.-]*:/i.test(path) && !/^[a-z]:[\\/]/i.test(path)) {
    return null;
  }

  path = path.split(/[?#]/, 1)[0];
  try { path = decodeURIComponent(path); } catch { return null; }
  if (!path || /[\u0000-\u001f\u007f]/.test(path)) return null;
  path = path.replace(/\\/g, "/");
  if (path.startsWith("//")) return null;
  if (path.startsWith("/workspace/")) path = path.slice(11);
  if (path.startsWith("./")) path = path.slice(2);
  if (path.split("/").includes("..")) return null;
  if (!/\.[a-z0-9]{1,12}$/i.test(path)) return null;
  return { path, agentId };
}

export function markdownUrlTransform(url, key, previewAvailable) {
  return key === "href" && previewAvailable && workspaceLinkTarget(url)
    ? url : defaultUrlTransform(url);
}
