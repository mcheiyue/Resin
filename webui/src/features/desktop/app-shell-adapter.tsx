import { Monitor } from "lucide-react";
import { Badge } from "../../components/ui/Badge";
import { getDesktopDefaultEntryPath, isDesktopSession, type ResinSessionKind } from "./session";

export type ShellNavItem = {
  label: string;
  path: string;
  icon: typeof Monitor;
};

type Translate = (value: string) => string;

export function buildShellNavItems(sessionKind: ResinSessionKind, navItems: ShellNavItem[]): ShellNavItem[] {
  if (!isDesktopSession(sessionKind)) {
    return navItems;
  }

  return [{ label: "桌面状态", path: getDesktopDefaultEntryPath(), icon: Monitor }, ...navItems];
}

export function renderDesktopBrandBadge(sessionKind: ResinSessionKind, t: Translate) {
  if (!isDesktopSession(sessionKind)) {
    return null;
  }

  return (
    <Badge className="brand-mode-tag" variant="info">
      {t("桌面")}
    </Badge>
  );
}

export function getDesktopSidebarHint(sessionKind: ResinSessionKind, token: string, t: Translate): string | null {
  if (isDesktopSession(sessionKind)) {
    return t("桌面会话由桌面壳注入，token 只保存在当前窗口内存中。");
  }

  if (!token) {
    return t("当前为免认证访问模式");
  }

  return null;
}

export function shouldShowBrowserLogout(sessionKind: ResinSessionKind, token: string): boolean {
  return !isDesktopSession(sessionKind) && token.trim().length > 0;
}
