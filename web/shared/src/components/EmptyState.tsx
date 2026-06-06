import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

interface Props {
  icon?: IconName | string;
  title: ReactNode;
  description?: ReactNode;
  desc?: ReactNode;
  action?: ReactNode;
  testId?: string;
}

export function EmptyState({ icon = "box", title, description, desc, action, testId }: Props) {
  const body = description ?? desc;
  return (
    <div className="empty" data-testid={testId}>
      <div className="empty__icon">
        <Icon name={icon} size={22} />
      </div>
      <h3 className="empty__title">{title}</h3>
      {body ? <p className="empty__desc">{body}</p> : null}
      {action ? <div style={{ marginTop: 4 }}>{action}</div> : null}
    </div>
  );
}
