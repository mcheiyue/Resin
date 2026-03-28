declare global {
  interface Window {
    go?: {
      main?: {
        wailsapp?: {
          App?: {
            ProxyAccessToken?: () => Promise<string>;
            OpenLogDirectory?: () => Promise<void>;
          };
        };
        App?: {
          ProxyAccessToken?: () => Promise<string>;
          OpenLogDirectory?: () => Promise<void>;
        };
      };
      wailsapp?: {
        App?: {
          ProxyAccessToken?: () => Promise<string>;
          OpenLogDirectory?: () => Promise<void>;
        };
      };
    };
  }
}

type DesktopAppBridge = {
  ProxyAccessToken?: () => Promise<string>;
  OpenLogDirectory?: () => Promise<void>;
};

function getDesktopAppBridge(): DesktopAppBridge | null {
  return (
    window.go?.main?.wailsapp?.App ??
    window.go?.wailsapp?.App ??
    window.go?.main?.App ??
    null
  );
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
