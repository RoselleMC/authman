export type ExtensionVisibility = "private" | "player_visible" | "public" | "admin_only";
export type ExtensionFieldType = "text" | "number" | "boolean" | "time" | "badge" | "list" | "link";
export type ExtensionTone = "neutral" | "success" | "warning" | "danger" | "info";

export interface ExtensionField {
  key: string;
  label: string;
  type: ExtensionFieldType;
  format?: string;
  visibility?: ExtensionVisibility;
  tone?: ExtensionTone;
  safe?: boolean;
}

export interface ExtensionSchema {
  version: 1;
  title: string;
  fields: ExtensionField[];
}

export interface ExtensionData {
  server_slug?: string;
  server_display_name?: string;
  provider: string;
  schema: ExtensionSchema;
  values: Record<string, unknown>;
  updated_at?: string;
}
