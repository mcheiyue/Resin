type DesktopBootstrapPayload = {
  desktop?: boolean;
  token?: string;
};

export type ResinSessionKind = "browser" | "desktop";

export type ResinDesktopBootstrap = {
  desktop: true;
  token: string;
};

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
  return isDesktopMode() ? "/desktop" : "/dashboard";
}
