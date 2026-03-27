import { create } from "zustand";
import { getDesktopBootstrapToken, isDesktopMode } from "../../lib/desktop-bootstrap";

const TOKEN_KEY = "resin_admin_token";

function loadInitialToken(): string {
  if (typeof window === "undefined") {
    return "";
  }
  if (isDesktopMode()) {
    return getDesktopBootstrapToken();
  }
  return window.localStorage.getItem(TOKEN_KEY) ?? "";
}

function syncBrowserToken(token: string): void {
  if (typeof window === "undefined" || isDesktopMode()) {
    return;
  }
  window.localStorage.setItem(TOKEN_KEY, token);
}

function clearBrowserToken(): void {
  if (typeof window === "undefined" || isDesktopMode()) {
    return;
  }
  window.localStorage.removeItem(TOKEN_KEY);
}

type AuthState = {
  token: string;
  sessionKind: "browser" | "desktop";
  setToken: (token: string) => void;
  clearToken: () => void;
};

export const useAuthStore = create<AuthState>((set) => ({
  token: loadInitialToken(),
  sessionKind: isDesktopMode() ? "desktop" : "browser",
  setToken: (token) => {
    const next = token.trim();
    syncBrowserToken(next);
    set({
      token: next,
      sessionKind: isDesktopMode() ? "desktop" : "browser",
    });
  },
  clearToken: () => {
    clearBrowserToken();
    set({
      token: "",
      sessionKind: isDesktopMode() ? "desktop" : "browser",
    });
  },
}));

export function getStoredAuthToken(): string {
  return useAuthStore.getState().token;
}
