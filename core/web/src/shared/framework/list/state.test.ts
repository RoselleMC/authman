import { describe, expect, it } from "vitest";
import {
  applyClientFilters,
  clampPage,
  makeDefaultState,
  paginateClient,
  readStateFromParams,
  totalPagesFor,
  visibleColumns,
  withPageReset,
  writeStateToParams,
} from "./state";
import type { ListColumn, ListState } from "./types";

interface Row {
  id: string;
  name: string;
  kind: "premium" | "offline";
  status: "active" | "locked";
}

const cols: ListColumn<Row>[] = [
  {
    key: "name",
    header: "Name",
    render: (r) => r.name,
    mandatory: true,
    filter: { type: "text" },
    getFilterValue: (r) => r.name,
  },
  {
    key: "kind",
    header: "Type",
    render: (r) => r.kind,
    filter: { type: "select", options: [{ value: "", label: "All" }, { value: "premium", label: "Premium" }, { value: "offline", label: "Offline" }] },
    getFilterValue: (r) => r.kind,
  },
  {
    key: "status",
    header: "Status",
    render: (r) => r.status,
    filter: { type: "select", options: [{ value: "", label: "All" }, { value: "active", label: "Active" }, { value: "locked", label: "Locked" }] },
    getFilterValue: (r) => r.status,
  },
  {
    key: "uuid",
    header: "UUID",
    render: (r) => r.id,
  },
];

const rows: Row[] = [
  { id: "1", name: "Steve", kind: "offline", status: "active" },
  { id: "2", name: "Alex", kind: "offline", status: "locked" },
  { id: "3", name: "Notch", kind: "premium", status: "active" },
  { id: "4", name: "jeb_", kind: "premium", status: "active" },
  { id: "5", name: "Dinnerbone", kind: "premium", status: "locked" },
];

describe("makeDefaultState", () => {
  it("returns a usable default", () => {
    const s = makeDefaultState();
    expect(s.page).toBe(1);
    expect(s.pageSize).toBe(25);
    expect(s.filters).toEqual({});
    expect(s.hidden).toEqual([]);
    expect(s.filtersVisible).toBe(false);
  });
  it("respects defaults", () => {
    const s = makeDefaultState({ pageSize: 50, hidden: ["uuid"], filtersVisible: true, filters: { kind: "premium" } });
    expect(s.pageSize).toBe(50);
    expect(s.hidden).toEqual(["uuid"]);
    expect(s.filtersVisible).toBe(true);
    expect(s.filters).toEqual({ kind: "premium" });
  });
});

describe("visibleColumns", () => {
  it("hides non-mandatory columns when state.hidden lists them", () => {
    const s: ListState = { ...makeDefaultState(), hidden: ["uuid", "status"] };
    const v = visibleColumns(cols, s);
    expect(v.map((c) => c.key)).toEqual(["name", "kind"]);
  });
  it("never hides mandatory columns even if listed", () => {
    const s: ListState = { ...makeDefaultState(), hidden: ["name", "kind"] };
    const v = visibleColumns(cols, s);
    expect(v.map((c) => c.key)).toContain("name");
  });
});

describe("applyClientFilters", () => {
  it("returns all rows when no filters set", () => {
    const out = applyClientFilters(rows, cols, makeDefaultState());
    expect(out).toHaveLength(5);
  });
  it("text filter does case-insensitive substring matching", () => {
    const s: ListState = { ...makeDefaultState(), filters: { name: "ste" } };
    expect(applyClientFilters(rows, cols, s).map((r) => r.id)).toEqual(["1"]);
  });
  it("select filter does exact match and chains with other filters", () => {
    const s: ListState = { ...makeDefaultState(), filters: { kind: "premium", status: "active" } };
    expect(applyClientFilters(rows, cols, s).map((r) => r.id).sort()).toEqual(["3", "4"]);
  });
  it("empty string filter is ignored", () => {
    const s: ListState = { ...makeDefaultState(), filters: { kind: "" } };
    expect(applyClientFilters(rows, cols, s)).toHaveLength(5);
  });
});

describe("paginateClient", () => {
  it("slices to the requested page", () => {
    const s: ListState = { ...makeDefaultState(), pageSize: 2, page: 2 };
    const out = paginateClient(rows, s);
    expect(out.map((r) => r.id)).toEqual(["3", "4"]);
  });
  it("returns the last page when totals don't divide evenly", () => {
    const s: ListState = { ...makeDefaultState(), pageSize: 2, page: 3 };
    expect(paginateClient(rows, s).map((r) => r.id)).toEqual(["5"]);
  });
});

describe("totalPagesFor + clampPage", () => {
  it("counts up correctly", () => {
    expect(totalPagesFor(0, 10)).toBe(1);
    expect(totalPagesFor(10, 10)).toBe(1);
    expect(totalPagesFor(11, 10)).toBe(2);
    expect(totalPagesFor(99, 10)).toBe(10);
  });
  it("clampPage snaps to range", () => {
    expect(clampPage(5, 3)).toBe(3);
    expect(clampPage(-1, 3)).toBe(1);
    expect(clampPage(2, 5)).toBe(2);
    expect(clampPage(NaN, 5)).toBe(1);
  });
});

describe("withPageReset", () => {
  it("returns a new state with page=1 by default", () => {
    const out = withPageReset({ ...makeDefaultState(), page: 5 });
    expect(out.page).toBe(1);
  });
  it("accepts a custom page", () => {
    const out = withPageReset({ ...makeDefaultState(), page: 5 }, 2);
    expect(out.page).toBe(2);
  });
});

describe("writeStateToParams / readStateFromParams", () => {
  it("round-trips a non-default state through URL params", () => {
    const s: ListState = { page: 3, pageSize: 50, filters: { kind: "premium", name: "ste" }, hidden: ["uuid"], filtersVisible: false };
    const u = new URLSearchParams();
    writeStateToParams(s, u, "p", { pageSize: 25 });
    expect(u.get("p.page")).toBe("3");
    expect(u.get("p.size")).toBe("50");
    expect(u.get("p.hidden")).toBe("uuid");
    expect(u.get("p.f.kind")).toBe("premium");
    expect(u.get("p.f.name")).toBe("ste");

    const back = readStateFromParams(u, "p", { pageSize: 25 });
    expect(back).toEqual(s);
  });
  it("omits default values to keep URLs clean", () => {
    const s: ListState = { page: 1, pageSize: 25, filters: {}, hidden: [], filtersVisible: false };
    const u = new URLSearchParams();
    writeStateToParams(s, u, "p", { pageSize: 25 });
    expect(u.toString()).toBe("");
  });
  it("preserves other unrelated params on the URL", () => {
    const u = new URLSearchParams("tab=identity&p.page=2");
    const back = readStateFromParams(u, "p");
    expect(back.page).toBe(2);
    writeStateToParams({ page: 1, pageSize: 25, filters: {}, hidden: [], filtersVisible: false }, u, "p", { pageSize: 25 });
    expect(u.get("tab")).toBe("identity");
    expect(u.get("p.page")).toBeNull();
  });
  it("supports an empty prefix for simple pages", () => {
    const s: ListState = { page: 4, pageSize: 50, filters: { kind: "premium" }, hidden: [], filtersVisible: false };
    const u = new URLSearchParams();
    writeStateToParams(s, u, "");
    expect(u.get("page")).toBe("4");
    expect(u.get("size")).toBe("50");
    expect(u.get("f.kind")).toBe("premium");
    expect(readStateFromParams(u, "")).toEqual(s);
  });
  it("survives garbage page / size values without throwing", () => {
    const u = new URLSearchParams("page=foo&size=bar&f.kind=offline");
    const back = readStateFromParams(u);
    expect(back.page).toBe(1);
    expect(back.pageSize).toBe(25);
    expect(back.filters.kind).toBe("offline");
  });
});
