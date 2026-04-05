import { create } from "zustand";
import {
  getDesktopBootstrapToken,
  getDesktopSessionKind,
  type ResinSessionKind,
} from "../../lib/desktop-bootstrap";

const TOKEN_KEY = "resin_admin_token";

function loadInitialToken(): string {
  if (typeof window === "undefined") {
    return "";
  }
  if (getDesktopSessionKind() === "desktop") {
    return getDesktopBootstrapToken();
  }
  return window.localStorage.getItem(TOKEN_KEY) ?? "";
}

function syncBrowserToken(token: string): void {
  if (typeof window === "undefined" || getDesktopSessionKind() === "desktop") {
    return;
  }
  window.localStorage.setItem(TOKEN_KEY, token);
}

function clearBrowserToken(): void {
  if (typeof window === "undefined" || getDesktopSessionKind() === "desktop") {
    return;
  }
  window.localStorage.removeItem(TOKEN_KEY);
}

type AuthState = {
  token: string;
  sessionKind: ResinSessionKind;
  setToken: (token: string) => void;
  clearToken: () => void;
};

export const useAuthStore = create<AuthState>((set) => ({
  token: loadInitialToken(),
  sessionKind: getDesktopSessionKind(),
  setToken: (token) => {
    const next = token.trim();
    syncBrowserToken(next);
    set({
      token: next,
      sessionKind: getDesktopSessionKind(),
    });
  },
  clearToken: () => {
    clearBrowserToken();
    set({
      token: "",
      sessionKind: getDesktopSessionKind(),
    });
  },
}));

export function getStoredAuthToken(): string {
  return useAuthStore.getState().token;
}
