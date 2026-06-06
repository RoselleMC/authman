import type { ReactNode } from "react";

/**
 * Outer wrapper for every unauthenticated player screen (login, register,
 * link, server-landing). Pairs with AuthHeader at the top.
 */
export function PAuth({ children }: { children: ReactNode }) {
  return <div className="pauth">{children}</div>;
}

interface AuthCardProps {
  children: ReactNode;
  /** Additional class names appended to .pauth-card (e.g. "link-card"). */
  variant?: "default" | "link";
  testId?: string;
}

/**
 * The card-shaped form container used by every player auth screen.
 * Use variant="link" for the slightly taller link-processing layout.
 */
export function AuthCard({ children, variant = "default", testId }: AuthCardProps) {
  const className = variant === "link" ? "pauth-card link-card" : "pauth-card";
  return (
    <div className="pauth-body">
      <div className={className} data-testid={testId}>
        {children}
      </div>
    </div>
  );
}

interface AuthHeadProps {
  title: ReactNode;
  desc?: ReactNode;
}

export function AuthHead({ title, desc }: AuthHeadProps) {
  return (
    <div className="pauth-head">
      <h1>{title}</h1>
      {desc ? <p>{desc}</p> : null}
    </div>
  );
}

export function AuthForm({ children, onSubmit }: { children: ReactNode; onSubmit?: (e: React.FormEvent) => void }) {
  return (
    <form className="pauth-form" onSubmit={onSubmit}>
      {children}
    </form>
  );
}
