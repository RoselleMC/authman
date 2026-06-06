import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

export interface TabItem<T extends string = string> {
  value: T;
  label: ReactNode;
  icon?: IconName | string;
  count?: number;
}

interface Props<T extends string = string> {
  value: T;
  onChange: (v: T) => void;
  tabs: ReadonlyArray<TabItem<T>>;
}

export function Tabs<T extends string = string>({ value, onChange, tabs }: Props<T>) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map((tab) => (
        <button
          key={tab.value}
          type="button"
          role="tab"
          className="tab"
          aria-selected={value === tab.value}
          onClick={() => onChange(tab.value)}
          data-testid={`tab-${tab.value}`}
        >
          {tab.icon ? <Icon name={tab.icon} size={15} /> : null}
          {tab.label}
          {tab.count != null ? (
            <span className="mono" style={{ fontSize: 11, opacity: 0.6 }}>
              {tab.count}
            </span>
          ) : null}
        </button>
      ))}
    </div>
  );
}
