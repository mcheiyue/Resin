import { create } from "zustand";
import { getCurrentDesktopSessionKind, getSessionAuthToken, shouldPersistBrowserToken, type ResinSessionKind } from "../desktop/session";

const TOKEN_KEY = "resin_admin_token";

function loadInitialToken(): string {
  if (typeof window === "undefined") {
    return "";
  }

  return getSessionAuthToken(window.localStorage.getItem(TOKEN_KEY) ?? "");
}

function syncBrowserToken(token: string): void {
  if (typeof window === "undefined" || !shouldPersistBrowserToken()) {
    return;
  }
  window.localStorage.setItem(TOKEN_KEY, token);
}

function clearBrowserToken(): void {
  if (typeof window === "undefined" || !shouldPersistBrowserToken()) {
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
  sessionKind: getCurrentDesktopSessionKind(),
  setToken: (token) => {
    const next = token.trim();
    syncBrowserToken(next);
    set({
      token: next,
      sessionKind: getCurrentDesktopSessionKind(),
    });
  },
  clearToken: () => {
    clearBrowserToken();
    set({
      token: "",
      sessionKind: getCurrentDesktopSessionKind(),
    });
  },
}));

export function getStoredAuthToken(): string {
  return useAuthStore.getState().token;
}
