/*
 * Authman frontend framework.
 *
 * This module exposes the page-level primitives that every admin and player
 * page must compose with. The component library under `../components/`
 * provides leaf controls (Button, Input, Card, …); this layer provides the
 * page chrome and the API-shape adapters.
 *
 * Rule for page authors: if a layout you need exists here, USE IT. Do not
 * hand-roll equivalent markup. Hand-rolled chrome drifts visually and bypasses
 * the safety net the framework provides (consistent test IDs, theme tokens,
 * coerced API values, etc.).
 */
export * from "./Page";
export * from "./Toolbar";
export * from "./StatGrid";
export * from "./HealthBanner";
export * from "./Detail";
export * from "./SecretReveal";
export * from "./ConfigGrid";
export * from "./PlaceholderCard";
export * from "./State";

export * from "./PContent";
export * from "./PAuth";
export * from "./ProtoPreview";
export * from "./StrengthMeter";
export * from "./LinkState";
export * from "./ServerContextChip";

export * from "./coerce";

export * from "./list";
