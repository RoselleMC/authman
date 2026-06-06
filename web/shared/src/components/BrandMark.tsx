interface Props {
  size?: number;
  sub?: string;
  /** When true, only show the mark; no brand-name label. */
  markOnly?: boolean;
}

/**
 * Authman brand: Roselle mark, then the wordmark
 * and an optional sub-label (e.g. "Admin").
 */
export function BrandMark({ size = 17, sub, markOnly }: Props) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 9 }}>
      <span className="brand-mark" aria-hidden="true" style={{ ["--brand-mark-size" as string]: `${Math.max(22, size + 11)}px` }} />
      {markOnly ? null : <span className="brand-name">Authman</span>}
      {sub ? <span className="brand-sub">{sub}</span> : null}
    </span>
  );
}
