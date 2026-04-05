type Translate = (value: string, variables?: Record<string, unknown>) => string;

type LoginCopy = {
  description: string;
  modeCallout: string;
  desktopHint: string | null;
};

export function getLoginCopy(desktopMode: boolean, defaultEntryPath: string, t: Translate): LoginCopy {
  if (!desktopMode) {
    return {
      description: t("如果你是直接在浏览器访问本机控制台，请输入当前 Admin Token；如果你已经在桌面版里，请返回桌面入口并点击“打开 Resin WebUI（桌面会话）”。"),
      modeCallout: t("浏览器直连只适合手工调试；桌面模式下推荐走桌面会话入口。"),
      desktopHint: null,
    };
  }

  return {
    description: t("桌面会话通常会自动接管登录；如果你仍看到这个页面，说明当前桌面会话没有完成注入，可以重新从桌面入口打开 Resin WebUI，或手动输入当前 Admin Token 继续。"),
    modeCallout: t("桌面推荐路径：从桌面壳入口进入 WebUI，无需手动输入 token。"),
    desktopHint: t("桌面模式下默认会话入口是 {{path}}，管理 token 只会保存在当前窗口内存中。", {
      path: defaultEntryPath,
    }),
  };
}
