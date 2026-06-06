import { IconButton } from "./IconButton";
import { useTheme } from "../theme/ThemeProvider";
import { useI18n } from "../i18n/I18nProvider";

export function ThemeToggle() {
  const { mode, toggle } = useTheme();
  const { t } = useI18n();
  return (
    <IconButton
      bordered
      name={mode === "dark" ? "sun" : "moon"}
      size={16}
      label={mode === "dark" ? t("common.theme.light") : t("common.theme.dark")}
      onClick={toggle}
      data-testid="theme-toggle"
    />
  );
}
