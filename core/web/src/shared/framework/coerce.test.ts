import { describe, expect, it } from "vitest";
import {
  asArray,
  asBoolean,
  asNumber,
  asString,
  coerceAdminUser,
  coerceAuditEvent,
  coerceMojangOverall,
  coerceMojangStatus,
  coerceSystemSummary,
  coerceVelocityNode,
  firstDefined,
  mapEnum,
} from "./coerce";

describe("primitive coercers", () => {
  it("asString accepts strings, numbers, booleans", () => {
    expect(asString("x")).toBe("x");
    expect(asString(42)).toBe("42");
    expect(asString(true)).toBe("true");
    expect(asString(null, "fb")).toBe("fb");
    expect(asString(undefined, "fb")).toBe("fb");
    expect(asString({}, "fb")).toBe("fb");
  });

  it("asNumber parses strings, rejects garbage", () => {
    expect(asNumber(7)).toBe(7);
    expect(asNumber("42")).toBe(42);
    expect(asNumber("3.14")).toBe(3.14);
    expect(asNumber("not a number", 9)).toBe(9);
    expect(asNumber(NaN, 1)).toBe(1);
    expect(asNumber(Infinity, 1)).toBe(1);
  });

  it("asBoolean handles 1/0 + true/false strings", () => {
    expect(asBoolean(true)).toBe(true);
    expect(asBoolean(false)).toBe(false);
    expect(asBoolean(1)).toBe(true);
    expect(asBoolean(0)).toBe(false);
    expect(asBoolean("true")).toBe(true);
    expect(asBoolean("false")).toBe(false);
    expect(asBoolean(null, true)).toBe(true);
  });

  it("asArray drops non-arrays and maps items", () => {
    expect(asArray<number>(null)).toEqual([]);
    expect(asArray<number>([1, 2, 3])).toEqual([1, 2, 3]);
    expect(asArray("nope")).toEqual([]);
    expect(asArray([1, 2], (x) => asNumber(x) * 2)).toEqual([2, 4]);
  });

  it("firstDefined returns the first present value", () => {
    expect(firstDefined<string>(undefined, null, "", "real")).toBe("real");
    expect(firstDefined<string>(undefined, null, "")).toBeUndefined();
  });

  it("mapEnum rejects unknown values", () => {
    expect(mapEnum("a", ["a", "b"] as const, "b")).toBe("a");
    expect(mapEnum("c", ["a", "b"] as const, "b")).toBe("b");
    expect(mapEnum(42, ["a", "b"] as const, "a")).toBe("a");
  });
});

describe("coerceSystemSummary", () => {
  it("fills missing fields with safe defaults instead of throwing", () => {
    const sys = coerceSystemSummary(null);
    expect(sys.version).toBe("0.0.0-dev");
    expect(sys.environment).toBe("unknown");
    expect(sys.database).toBe("unknown");
    expect(sys.uptime_seconds).toBeNull();
    expect(sys.feature_flags).toEqual({});
    expect(sys.extra_rows).toEqual([]);
  });

  it("accepts canonical fields", () => {
    const sys = coerceSystemSummary({
      version: "1.2.3",
      environment: "production",
      database: "postgres",
      uptime_seconds: 120,
      feature_flags: { audit: true, beta: false },
    });
    expect(sys.version).toBe("1.2.3");
    expect(sys.environment).toBe("production");
    expect(sys.database).toBe("postgres");
    expect(sys.uptime_seconds).toBe(120);
    expect(sys.feature_flags).toEqual({ audit: true, beta: false });
  });

  it("accepts alternate field names (env, db, app_version)", () => {
    const sys = coerceSystemSummary({
      app_version: "2.0.0",
      env: "staging",
      db: "postgres-15",
      uptime: 99,
    });
    expect(sys.version).toBe("2.0.0");
    expect(sys.environment).toBe("staging");
    expect(sys.database).toBe("postgres-15");
    expect(sys.uptime_seconds).toBe(99);
  });

  it("collects unknown scalar fields under extra_rows", () => {
    const sys = coerceSystemSummary({
      version: "1.0.0",
      region: "eu-west",
      replica_count: 3,
    });
    expect(sys.extra_rows).toContainEqual({ k: "region", v: "eu-west" });
    expect(sys.extra_rows).toContainEqual({ k: "replica_count", v: "3" });
  });
});

describe("coerceMojangOverall", () => {
  it("classifies known healthy keys as success", () => {
    expect(coerceMojangOverall("direct_healthy").tone).toBe("success");
    expect(coerceMojangOverall("proxy_healthy").tone).toBe("success");
    expect(coerceMojangOverall("mojang_healthy").tone).toBe("success");
  });

  it("classifies degraded keys as warning", () => {
    expect(coerceMojangOverall("proxy_rate_limited").tone).toBe("warning");
    expect(coerceMojangOverall("mojang_degraded").tone).toBe("warning");
    expect(coerceMojangOverall("mojang_disabled").tone).toBe("warning");
  });

  it("classifies failure keys as danger", () => {
    expect(coerceMojangOverall("proxy_failed").tone).toBe("danger");
    expect(coerceMojangOverall("mojang_unavailable").tone).toBe("danger");
    expect(coerceMojangOverall("mojang_unavailable_nocache").tone).toBe("danger");
  });

  it("falls back to warning for unknown keys (does not throw)", () => {
    expect(coerceMojangOverall("some_new_state").tone).toBe("warning");
    expect(coerceMojangOverall(null).tone).toBe("warning");
    expect(coerceMojangOverall(undefined).tone).toBe("warning");
  });
});

describe("coerceMojangStatus", () => {
  it("returns empty arrays + neutral overall on null input", () => {
    const s = coerceMojangStatus(null);
    expect(s.proxies).toEqual([]);
    expect(s.events).toEqual([]);
    expect(s.overall.tone).toBe("warning");
  });

  it("accepts a sparse response without throwing", () => {
    const s = coerceMojangStatus({
      overall: "mojang_disabled",
      proxies: [{ id: "p1", kind: "direct" }],
      events: [{ id: "e1", event_type: "rate_limited" }],
    });
    expect(s.overall).toEqual({ key: "mojang_disabled", tone: "warning" });
    expect(s.proxies[0]?.id).toBe("p1");
    expect(s.proxies[0]?.url_masked).toBe("(direct)");
    expect(s.proxies[0]?.cooldown_remaining_seconds).toBe(0);
    expect(s.events[0]?.proxy_id).toBe("—");
  });

  it("normalizes request count across legacy field names", () => {
    const s = coerceMojangStatus({
      proxies: [
        { id: "p1", kind: "http", failure_count: 17 },
        { id: "p2", kind: "http", rate_limit_count: 3 },
        { id: "p3", kind: "http", recent_request_count: 1234 },
      ],
    });
    expect(s.proxies.map((p) => p.request_count)).toEqual([17, 3, 1234]);
  });
});

describe("coerceAdminUser", () => {
  it("survives missing optional fields", () => {
    const u = coerceAdminUser({ id: "u1", email: "x@example.invalid", role: "auditor" });
    expect(u.id).toBe("u1");
    expect(u.display_name).toBe("x@example.invalid");
    expect(u.status).toBe("active");
    expect(u.created_at).toBeNull();
  });

  it("preserves custom role ids", () => {
    const u = coerceAdminUser({ id: "u1", role: "support.readonly", role_alias: "Support" });
    expect(u.role).toBe("support.readonly");
    expect(u.role_alias).toBe("Support");
  });

  it("accepts the legacy `name` field", () => {
    const u = coerceAdminUser({ id: "u1", name: "Sam" });
    expect(u.display_name).toBe("Sam");
  });
});

describe("coerceVelocityNode", () => {
  it("accepts a sparse node with no heartbeat / fingerprint", () => {
    const n = coerceVelocityNode({ id: "n1" });
    expect(n.id).toBe("n1");
    expect(n.token_fingerprint).toBe("—");
    expect(n.last_seen_at).toBeNull();
    expect(n.status).toBe("stale");
  });

  it("accepts legacy `fingerprint` alias", () => {
    const n = coerceVelocityNode({ id: "n1", fingerprint: "a1b2…" });
    expect(n.token_fingerprint).toBe("a1b2…");
  });

  it("coerces limbo protocol pack state without trusting response shapes", () => {
    const n = coerceVelocityNode({
      id: "n1",
      kind: "limbo_portal",
      protocol_pack: {
        source: "custom",
        in_sync: true,
        configured: {
          name: "release-pack",
          version: "2026.07",
          protocols: [771, "774", "bad"],
          minecraft_versions: ["1.21.6", "1.21.11", null],
        },
      },
    });
    expect(n.protocol_pack?.source).toBe("custom");
    expect(n.protocol_pack?.in_sync).toBe(true);
    expect(n.protocol_pack?.configured?.protocols).toEqual([771, 774]);
    expect(n.protocol_pack?.configured?.minecraft_versions).toEqual(["1.21.6", "1.21.11"]);
    expect(n.protocol_pack?.active).toBeNull();
  });
});

describe("coerceAuditEvent", () => {
  it("provides sensible labels when actor / target are missing", () => {
    const ev = coerceAuditEvent({ event_type: "player.locked" });
    expect(ev.event_type).toBe("player.locked");
    expect(ev.actor_label).toBe("—");
    expect(ev.target_label).toBe("—");
    expect(ev.actor_type).toBe("system");
  });
});
