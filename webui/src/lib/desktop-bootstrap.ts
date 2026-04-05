type DesktopBootstrapPayload = {
  desktop?: boolean;
  token?: string;
  defaultPath?: string;
  helpPath?: string;
};

export type ResinSessionKind = "browser" | "desktop";

export type ResinDesktopBootstrap = {
  desktop: true;
  token: string;
  defaultPath: string;
  helpPath: string;
};

const desktopStatusPath = "/desktop";
const desktopHelpPath = "/desktop/help";

declare global {
  interface Window {
    __RESIN_DESKTOP_BOOTSTRAP__?: DesktopBootstrapPayload;
  }
}

function normalizeDesktopBootstrap(raw: DesktopBootstrapPayload | undefined): ResinDesktopBootstrap | null {
  const token = raw?.token?.trim() ?? "";
  if (raw?.desktop !== true || !token) {
    return null;
  }
  return {
    desktop: true,
    token,
    defaultPath: raw.defaultPath?.trim() || desktopStatusPath,
    helpPath: raw.helpPath?.trim() || desktopHelpPath,
  };
}

export function getDesktopBootstrap(): ResinDesktopBootstrap | null {
  if (typeof window === "undefined") {
    return null;
  }
  return normalizeDesktopBootstrap(window.__RESIN_DESKTOP_BOOTSTRAP__);
}

export function isDesktopMode(): boolean {
  return getDesktopBootstrap()?.desktop === true;
}

export function getDesktopBootstrapToken(): string {
  return getDesktopBootstrap()?.token ?? "";
}

export function getDesktopSessionKind(): ResinSessionKind {
  return isDesktopMode() ? "desktop" : "browser";
}

export function getDefaultAppPath(): string {
  return getDesktopBootstrap()?.defaultPath ?? "/dashboard";
}

export function getDesktopHelpPath(): string {
  return getDesktopBootstrap()?.helpPath ?? desktopHelpPath;
}
