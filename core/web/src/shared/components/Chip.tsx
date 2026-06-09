import type { ReactNode } from "react";

interface Props {
  active?: boolean;
  count?: number;
  onClick?: () => void;
  children: ReactNode;
  testId?: string;
}

export function Chip({ active, count, onClick, children, testId }: Props) {
  return (
    <button
      type="button"
      className="chip"
      aria-pressed={!!active}
      onClick={onClick}
      data-testid={testId}
    >
      {children}
      {count != null ? <span className="chip-count">{count}</span> : null}
    </button>
  );
}
