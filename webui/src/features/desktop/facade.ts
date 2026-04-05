import { getDefaultAppPath, getDesktopHelpPath, isDesktopMode } from "../../lib/desktop-bootstrap";
import {
  copyDesktopDiagnostics,
  getDesktopProxyAccessToken,
  hasDesktopAppBridge,
  openDesktopLogDirectory,
} from "../../lib/desktop-bridge";
import { useAuthStore } from "../auth/auth-store";

export function useDesktopFacadeState() {
  const token = useAuthStore((state) => state.token);
  const sessionKind = useAuthStore((state) => state.sessionKind);

  return {
    token,
    sessionKind,
    desktopMode: isDesktopMode(),
    desktopHomePath: getDefaultAppPath(),
    desktopHelpPath: getDesktopHelpPath(),
    bridgeAvailable: hasDesktopAppBridge(),
  };
}

export async function readDesktopProxyToken(): Promise<string> {
  return getDesktopProxyAccessToken();
}

export async function triggerDesktopLogDirectoryOpen(): Promise<boolean> {
  return openDesktopLogDirectory();
}

export async function readDesktopDiagnostics(): Promise<string> {
  return copyDesktopDiagnostics();
}
