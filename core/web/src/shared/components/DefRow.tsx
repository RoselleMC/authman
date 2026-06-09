import type { ReactNode } from "react";

interface Props {
  k: ReactNode;
  children: ReactNode;
}

export function DefRow({ k, children }: Props) {
  return (
    <div className="defrow">
      <div className="defrow__k">{k}</div>
      <div className="defrow__v">{children}</div>
    </div>
  );
}

export function DefList({ children }: { children: ReactNode }) {
  return <div className="deflist">{children}</div>;
}
