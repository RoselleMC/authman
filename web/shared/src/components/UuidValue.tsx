import { Copyable } from "./Copyable";

interface Props {
  uuid: string;
  truncate?: boolean | number;
}

export function UuidValue({ uuid, truncate }: Props) {
  const limit = typeof truncate === "number" ? truncate : truncate ? 13 : undefined;
  return <Copyable value={uuid} truncate={limit} testId="uuid-value" />;
}
