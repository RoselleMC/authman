import type { CSSProperties } from "react";

interface Props {
  rawName: string;
  kind?: "premium" | "offline";
  /** Backwards-compat: if protocolOnly is true treat as offline form. */
  protocolOnly?: boolean;
  style?: CSSProperties;
}

/**
 * Offline protocol name = "#" + raw, with the "#" rendered in brand green.
 * Premium names have no prefix — Mojang controls the display name.
 */
export function ProtocolName({ rawName, kind, style }: Props) {
  const isPremium = kind === "premium";
  if (isPremium) {
    return (
      <span className="protoname" style={style}>
        <span className="proto-body">{rawName}</span>
      </span>
    );
  }
  return (
    <span className="protoname" title={`Offline protocol name: #${rawName}`} style={style}>
      <span className="proto-hash">#</span>
      <span className="proto-body">{rawName}</span>
    </span>
  );
}
