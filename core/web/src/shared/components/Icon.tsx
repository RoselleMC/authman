import type { CSSProperties } from "react";

const PATHS: Record<string, string> = {
  search: "M11 11l4 4 M7.5 13a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11Z",
  close: "M5 5l10 10 M15 5L5 15",
  check: "M4 10.5l4 4 8-9",
  copy: "M7 7h7v9H7zM5 11H3V3h8v2",
  chevronDown: "M5 8l5 5 5-5",
  chevronUp: "M5 12l5-5 5 5",
  chevronRight: "M8 5l5 5-5 5",
  chevronLeft: "M12 5l-5 5 5 5",
  sun: "M10 3v2 M10 15v2 M3 10h2 M15 10h2 M5.2 5.2l1.4 1.4 M13.4 13.4l1.4 1.4 M14.8 5.2l-1.4 1.4 M6.6 13.4l-1.4 1.4 M10 7a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z",
  moon: "M16 11.5A6.5 6.5 0 0 1 8.5 4 6.5 6.5 0 1 0 16 11.5Z",
  lock: "M5.5 9h9v6.5h-9zM7 9V6.5a3 3 0 0 1 6 0V9",
  unlock: "M5.5 9h9v6.5h-9zM7 9V6.5a3 3 0 0 1 5.9-.8",
  refresh: "M15.5 6.5A6 6 0 1 0 16 10 M15.5 3v3.5H12",
  rotate: "M14.5 7A5.5 5.5 0 1 0 15 10.5 M14.5 3.5V7H11",
  user: "M10 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM4.5 16a5.5 5.5 0 0 1 11 0",
  users: "M7 9a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5ZM2.5 15.5a4.5 4.5 0 0 1 9 0 M13 9.2a2.4 2.4 0 0 0 0-4.4 M14 15.5a4.5 4.5 0 0 0-2.2-3.9",
  shield: "M10 3l6 2.2v4.3c0 3.8-2.6 6.5-6 7.7-3.4-1.2-6-3.9-6-7.7V5.2L10 3Z",
  server: "M3.5 4.5h13v4h-13zM3.5 11.5h13v4h-13z M6 6.5h.01 M6 13.5h.01",
  layers: "M10 3l7 3.5-7 3.5-7-3.5L10 3Z M3 11l7 3.5 7-3.5",
  activity: "M3 10h3l2-5 4 10 2-5h3",
  gauge: "M10 16a6 6 0 1 1 6-6 M10 10l3-2.5",
  list: "M6 5.5h10 M6 10h10 M6 14.5h10 M3 5.5h.01 M3 10h.01 M3 14.5h.01",
  settings:
    "M8.7 2.8h2.6l.4 2c.4.1.8.3 1.2.5l1.7-1.1 1.8 1.8-1.1 1.7c.2.4.4.8.5 1.2l2 .4v2.6l-2 .4c-.1.4-.3.8-.5 1.2l1.1 1.7-1.8 1.8-1.7-1.1c-.4.2-.8.4-1.2.5l-.4 2H8.7l-.4-2c-.4-.1-.8-.3-1.2-.5l-1.7 1.1-1.8-1.8 1.1-1.7a6 6 0 0 1-.5-1.2l-2-.4V9.3l2-.4c.1-.4.3-.8.5-1.2L3.6 6l1.8-1.8 1.7 1.1c.4-.2.8-.4 1.2-.5l.4-2Z M10 12.6a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2Z",
  link: "M8.5 11.5l3-3 M7.5 12.5l-1 1a2.1 2.1 0 0 1-3-3l2-2a2.1 2.1 0 0 1 3 0 M12.5 7.5l1-1a2.1 2.1 0 0 1 3 3l-2 2a2.1 2.1 0 0 1-3 0",
  alert: "M10 3l7 13H3L10 3Z M10 8v3.5 M10 13.7h.01",
  info: "M10 17a7 7 0 1 0 0-14 7 7 0 0 0 0 14ZM10 9v4.5 M10 6.6h.01",
  clock: "M10 17a7 7 0 1 0 0-14 7 7 0 0 0 0 14ZM10 6.5V10l2.5 1.5",
  eye: "M2.5 10S5.5 5 10 5s7.5 5 7.5 5-3 5-7.5 5-7.5-5-7.5-5Z M10 12a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z",
  eyeOff:
    "M4 4l12 12 M8.2 8.3A2 2 0 0 0 10 12a2 2 0 0 0 1.8-1.2 M6.3 6.4C4 7.7 2.5 10 2.5 10S5.5 15 10 15c1 0 1.9-.2 2.7-.6 M9 5.1C9.3 5 9.6 5 10 5c4.5 0 7.5 5 7.5 5a13 13 0 0 1-1.7 2.2",
  plus: "M10 4v12 M4 10h12",
  trash: "M7 5V3.5h6V5 M4.5 5h11 M6 7v10h8V7 M8.5 9v4.5 M11.5 9v4.5",
  filter: "M3.5 5h13l-5 6v4l-3 1.5V11L3.5 5Z",
  logout: "M12 6V4.5h-7v11h7V14 M8.5 10h8 M14 7.5l2.5 2.5L14 12.5",
  grid: "M4 4h5v5H4zM11 4h5v5h-5zM4 11h5v5H4zM11 11h5v5h-5z",
  arrowRight: "M4 10h11 M11 6l4 4-4 4",
  arrowLeft: "M16 10H5 M9 6l-4 4 4 4",
  external: "M8 5H5v10h10v-3 M11.5 4.5H16v4.5 M16 4.5l-6 6",
  dot: "M10 12a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z",
  fingerprint: "M6 9a4 4 0 0 1 8 0v1 M8 9a2 2 0 0 1 4 0v1.5a6 6 0 0 1-.5 2.5 M6 11a8 8 0 0 0 1 4 M10 9v2.5a4 4 0 0 0 1 2.8 M4.5 7a7 7 0 0 1 11 0",
  key: "M11.5 5.5a3 3 0 1 1-2.3 4.9L4.5 15v1.5H7l.5-1.5h1.5l.5-1.5h1.2",
  box: "M10 3l7 3.5v7L10 17l-7-3.5v-7L10 3Z M3 6.5l7 3.5 7-3.5 M10 10v7",
  database:
    "M10 6.5c3.6 0 6.5-1 6.5-2.2S13.6 2 10 2 3.5 3 3.5 4.3 6.4 6.5 10 6.5Z M3.5 4.3v11.4C3.5 17 6.4 18 10 18s6.5-1 6.5-2.3V4.3 M3.5 10c0 1.3 2.9 2.3 6.5 2.3s6.5-1 6.5-2.3",
  globe: "M10 17a7 7 0 1 0 0-14 7 7 0 0 0 0 14Z M3.5 10h13 M10 3c2 1.9 3 4.2 3 7s-1 5.1-3 7 M10 3c-2 1.9-3 4.2-3 7s1 5.1 3 7",
};

export type IconName = keyof typeof PATHS;

interface Props {
  name: IconName | string;
  size?: number;
  strokeWidth?: number;
  className?: string;
  style?: CSSProperties;
}

export function Icon({ name, size = 16, strokeWidth = 1.6, className, style }: Props) {
  const d = PATHS[name];
  if (!d) return null;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
      style={{ display: "block", flexShrink: 0, ...style }}
    >
      <path d={d} />
    </svg>
  );
}
