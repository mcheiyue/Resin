declare global {
  interface Window {
    go?: {
      main?: {
        wailsapp?: {
          App?: {
            ProxyAccessToken?: () => Promise<string>;
            OpenLogDirectory?: () => Promise<void>;
            CopyDiagnostics?: () => Promise<string> | string;
          };
        };
        App?: {
          ProxyAccessToken?: () => Promise<string>;
          OpenLogDirectory?: () => Promise<void>;
          CopyDiagnostics?: () => Promise<string> | string;
        };
      };
      wailsapp?: {
        App?: {
          ProxyAccessToken?: () => Promise<string>;
          OpenLogDirectory?: () => Promise<void>;
          CopyDiagnostics?: () => Promise<string> | string;
        };
      };
    };
  }
}

type DesktopAppBridge = {
  ProxyAccessToken?: () => Promise<string>;
  OpenLogDirectory?: () => Promise<void>;
  CopyDiagnostics?: () => Promise<string> | string;
};

function getDesktopAppBridge(): DesktopAppBridge | null {
  return (
    window.go?.main?.wailsapp?.App ??
    window.go?.wailsapp?.App ??
    window.go?.main?.App ??
    null
  );
}

export function hasDesktopAppBridge(): boolean {
  return getDesktopAppBridge() !== null;
}

export async function getDesktopProxyAccessToken(): Promise<string> {
  const bridge = getDesktopAppBridge();
  if (!bridge?.ProxyAccessToken) {
    return "";
  }
  return (await bridge.ProxyAccessToken()).trim();
}

export async function openDesktopLogDirectory(): Promise<boolean> {
  const bridge = getDesktopAppBridge();
  if (!bridge?.OpenLogDirectory) {
    return false;
  }
  await bridge.OpenLogDirectory();
  return true;
}

export async function copyDesktopDiagnostics(): Promise<string> {
  const bridge = getDesktopAppBridge();
  if (!bridge?.CopyDiagnostics) {
    return "";
  }

  const result = await bridge.CopyDiagnostics();
  return String(result ?? "").trim();
}
