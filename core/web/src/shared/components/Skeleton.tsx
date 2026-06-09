interface Props {
  width?: number | string;
  height?: number | string;
  radius?: number | string;
}

export function Skeleton({ width = "100%", height = 14, radius }: Props) {
  return (
    <div
      className="skel"
      aria-hidden="true"
      style={{
        display: "inline-block",
        width,
        height,
        borderRadius: radius,
      }}
    />
  );
}
