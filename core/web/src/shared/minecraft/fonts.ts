export type MinecraftVanillaFontID =
  | "minecraft:default"
  | "minecraft:uniform"
  | "minecraft:alt"
  | "minecraft:illageralt";

export interface MinecraftFontInfo {
  id: string;
  short: string;
  label: string;
  vanilla: boolean;
  internal?: boolean;
}
export const MINECRAFT_VANILLA_FONTS: MinecraftFontInfo[] = [
  { id: "minecraft:default", short: "default", label: "Default", vanilla: true },
  { id: "minecraft:uniform", short: "uniform", label: "Uniform", vanilla: true },
  { id: "minecraft:alt", short: "alt", label: "Alt", vanilla: true },
  { id: "minecraft:illageralt", short: "illageralt", label: "Illageralt", vanilla: true },
];

export const MINECRAFT_INTERNAL_FONT_IDS = [
  "minecraft:include/default",
  "minecraft:include/space",
  "minecraft:include/unifont",
] as const;

const VANILLA_BY_ID = new Map(MINECRAFT_VANILLA_FONTS.map((font) => [font.id, font]));
const VANILLA_BY_SHORT = new Map(MINECRAFT_VANILLA_FONTS.map((font) => [font.short, font]));

export function normalizeMinecraftFontID(raw: string): string {
  const clean = raw.trim().replace(/^['"]|['"]$/g, "").toLowerCase();
  if (!clean) return "";
  if (clean.includes(":")) return clean;
  return `minecraft:${clean}`;
}

export function minecraftFontInfo(raw: string): MinecraftFontInfo | null {
  const id = normalizeMinecraftFontID(raw);
  if (!id) return null;
  const vanilla = VANILLA_BY_ID.get(id);
  if (vanilla) return vanilla;
  if (id.startsWith("minecraft:")) {
    const short = id.slice("minecraft:".length);
    const byShort = VANILLA_BY_SHORT.get(short);
    if (byShort) return byShort;
    return { id, short, label: short, vanilla: false, internal: MINECRAFT_INTERNAL_FONT_IDS.includes(id as typeof MINECRAFT_INTERNAL_FONT_IDS[number]) };
  }
  return { id, short: id, label: id, vanilla: false };
}
