import { Icon, type IconName } from "./Icon";

export interface SegmentedOption<T extends string = string> {
  value: T;
  label: string;
  icon?: IconName | string;
}

interface Props<T extends string = string> {
  value: T;
  onChange: (v: T) => void;
  options: ReadonlyArray<SegmentedOption<T>>;
  ariaLabel?: string;
}

export function Segmented<T extends string = string>({ value, onChange, options, ariaLabel }: Props<T>) {
  return (
    <div className="segmented" role="group" aria-label={ariaLabel}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          className="segmented__opt"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
        >
          {o.icon ? <Icon name={o.icon} size={14} /> : null}
          {o.label}
        </button>
      ))}
    </div>
  );
}
