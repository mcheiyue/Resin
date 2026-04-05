import { getDefaultAppPath, getDesktopBootstrapToken, getDesktopHelpPath, getDesktopSessionKind, type ResinSessionKind } from "../../lib/desktop-bootstrap";

export type { ResinSessionKind };

export function isDesktopSession(sessionKind: ResinSessionKind): boolean {
  return sessionKind === "desktop";
}

export function getDesktopDefaultEntryPath(): string {
  return getDefaultAppPath();
}

export function getDesktopHelpRoute(): string {
  return getDesktopHelpPath();
}

export function getCurrentDesktopSessionKind(): ResinSessionKind {
  return getDesktopSessionKind();
}

export function getSessionAuthToken(browserToken: string): string {
  if (isDesktopSession(getCurrentDesktopSessionKind())) {
    return getDesktopBootstrapToken();
  }

  return browserToken;
}

export function shouldPersistBrowserToken(): boolean {
  return !isDesktopSession(getCurrentDesktopSessionKind());
}

export function resolveNextPath(search: string): string {
  const params = new URLSearchParams(search);
  return params.get("next") || getDesktopDefaultEntryPath();
}
