import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const port = Number.parseInt(process.env.PORT || "8080", 10);
const coreURL = normalizeURL(process.env.AUTHMAN_CORE_URL || "http://authman:8080");
const externalAPIToken = (process.env.AUTHMAN_EXTERNAL_API_TOKEN || "").trim();
const staticDir = process.env.STATIC_DIR || path.resolve(__dirname, "../frontend/dist");
const indexFile = path.join(staticDir, "index.html");

const hopHeaders = new Set([
  "connection",
  "expect",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailers",
  "transfer-encoding",
  "upgrade"
]);

const mimeTypes = new Map([
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".mjs", "text/javascript; charset=utf-8"],
  [".css", "text/css; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".svg", "image/svg+xml"],
  [".png", "image/png"],
  [".jpg", "image/jpeg"],
  [".jpeg", "image/jpeg"],
  [".webp", "image/webp"],
  [".ico", "image/x-icon"]
]);

const server = http.createServer(async (req, res) => {
  try {
    const parsed = new URL(req.url || "/", "http://example.local");
    if (parsed.pathname === "/healthz") {
      writeJSON(res, 200, { ok: true, service: "authman-player-panel-example" });
      return;
    }
    if (parsed.pathname.startsWith("/api/")) {
      await handleAPI(req, res, parsed);
      return;
    }
    await serveStatic(res, parsed.pathname);
  } catch (err) {
    console.error(err);
    writeJSON(res, 500, { error: { code: "example.internal", message: "example panel failed" } });
  }
});

server.listen(port, "0.0.0.0", () => {
  console.log(`player panel example listening on :${port}`);
  console.log(`proxying Authman Core player API to ${coreURL}`);
});

async function handleAPI(req, res, parsed) {
  if (parsed.pathname === "/api/_example/status") {
    await handleStatus(res);
    return;
  }
  if (!isAllowedPortalPath(parsed.pathname)) {
    writeJSON(res, 404, {
      error: {
        code: "example.unsupported_api",
        message: "this example only proxies Authman Core public portal APIs"
      }
    });
    return;
  }
  if (!externalAPIToken) {
    writeJSON(res, 503, {
      error: {
        code: "example.external_token_missing",
        message: "AUTHMAN_EXTERNAL_API_TOKEN is required for player panel API access"
      }
    });
    return;
  }

  const target = new URL(parsed.pathname + parsed.search, coreURL);
  const headers = {};
  for (const [name, value] of Object.entries(req.headers)) {
    const lower = name.toLowerCase();
    if (value === undefined || hopHeaders.has(lower)) {
      continue;
    }
    if (lower === "host") {
      continue;
    }
    if (lower === "authorization" || lower === "x-authman-external-token") {
      continue;
    }
    headers[name] = Array.isArray(value) ? value.join(", ") : value;
  }
  headers.authorization = `Bearer ${externalAPIToken}`;
  headers["x-forwarded-host"] = req.headers.host || "";
  headers["x-forwarded-proto"] = req.socket.encrypted ? "https" : "http";

  const upstream = await fetch(target, {
    method: req.method,
    headers,
    body: hasBody(req.method) ? req : undefined,
    duplex: hasBody(req.method) ? "half" : undefined,
    redirect: "manual"
  });

  res.statusCode = upstream.status;
  upstream.headers.forEach((value, name) => {
    if (hopHeaders.has(name) || name === "set-cookie") {
      return;
    }
    res.setHeader(name, value);
  });
  const setCookies = typeof upstream.headers.getSetCookie === "function"
    ? upstream.headers.getSetCookie()
    : [];
  if (setCookies.length > 0) {
    res.setHeader("Set-Cookie", setCookies);
  } else {
    const cookie = upstream.headers.get("set-cookie");
    if (cookie) {
      res.setHeader("Set-Cookie", cookie);
    }
  }
  res.end(Buffer.from(await upstream.arrayBuffer()));
}

async function handleStatus(res) {
  const target = new URL("/healthz", coreURL);
  try {
    const upstream = await fetch(target, { method: "GET" });
    writeJSON(res, 200, {
      core_url_configured: true,
      external_token_configured: externalAPIToken.length > 0,
      core_health_status: upstream.status,
      core_reachable: upstream.ok
    });
  } catch {
    writeJSON(res, 200, {
      core_url_configured: true,
      external_token_configured: externalAPIToken.length > 0,
      core_health_status: null,
      core_reachable: false
    });
  }
}

async function serveStatic(res, pathname) {
  const decoded = decodeURIComponent(pathname);
  const requested = decoded === "/" ? indexFile : path.join(staticDir, decoded);
  const resolved = path.resolve(requested);
  const root = path.resolve(staticDir);
  if (resolved !== root && !resolved.startsWith(root + path.sep)) {
    writeJSON(res, 403, { error: { code: "example.forbidden", message: "forbidden" } });
    return;
  }
  const candidate = await statFile(resolved) ? resolved : indexFile;
  if (!(await statFile(candidate))) {
    writeJSON(res, 404, { error: { code: "example.not_built", message: "frontend assets are missing" } });
    return;
  }
  const ext = path.extname(candidate);
  res.statusCode = 200;
  res.setHeader("Content-Type", mimeTypes.get(ext) || "application/octet-stream");
  fs.createReadStream(candidate).pipe(res);
}

async function statFile(file) {
  try {
    const stat = await fs.promises.stat(file);
    return stat.isFile();
  } catch {
    return false;
  }
}

function isAllowedPortalPath(pathname) {
  return (
    pathname === "/api/portal" ||
    pathname.startsWith("/api/portal/") ||
    pathname.startsWith("/api/assets/profiles/") ||
    pathname.startsWith("/api/assets/passports/") ||
    pathname.startsWith("/api/assets/default-skins/")
  );
}

function hasBody(method = "GET") {
  return !["GET", "HEAD"].includes(method.toUpperCase());
}

function writeJSON(res, status, payload) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json; charset=utf-8");
  res.end(JSON.stringify(payload));
}

function normalizeURL(value) {
  return value.endsWith("/") ? value : `${value}/`;
}
