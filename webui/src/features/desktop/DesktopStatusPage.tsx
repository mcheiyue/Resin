import { useNavigate } from "react-router-dom";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { useI18n } from "../../i18n";
import { isDesktopMode } from "../../lib/desktop-bootstrap";
import { useAuthStore } from "../auth/auth-store";

function maskToken(token: string): string {
  const trimmed = token.trim();
  if (!trimmed) {
    return "未注入";
  }
  if (trimmed.length <= 10) {
    return `${trimmed.slice(0, 4)}…`;
  }
  return `${trimmed.slice(0, 6)}…${trimmed.slice(-4)}`;
}

export function DesktopStatusPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const token = useAuthStore((state) => state.token);
  const sessionKind = useAuthStore((state) => state.sessionKind);
  const desktopMode = isDesktopMode();

  return (
    <section className="desktop-status-page">
      <header className="module-header">
        <div>
          <h2>{t("桌面状态")}</h2>
          <p className="module-description">
            {desktopMode
              ? t("当前窗口由桌面壳驱动，业务请求仍走 /api/ Bearer 头；注入的 admin token 只保留在当前页面内存中。")
              : t("当前未检测到桌面壳注入对象，因此仍按浏览器模式处理登录与本地持久化。")}
          </p>
        </div>

        <div className="desktop-status-headline">
          <Badge variant={desktopMode ? "info" : "warning"}>{desktopMode ? t("桌面模式") : t("浏览器模式")}</Badge>
          <Badge variant={sessionKind === "desktop" ? "success" : "muted"}>
            {sessionKind === "desktop" ? t("仅内存会话") : t("浏览器会话")}
          </Badge>
        </div>
      </header>

      <Card className="desktop-status-hero">
        <h3>{t("桌面桥接入口已就绪")}</h3>
        <p>
          {desktopMode
            ? t("桌面模式会把 /ui/ 根入口优先落到这个状态页，再通过侧边导航进入原有业务页面；这样能保持业务流不变，同时把桌面态状态和认证桥接显式暴露出来。")
            : t("这个页面同样可以在浏览器模式下打开，用来核对当前是否已切到桌面态桥接。")}
        </p>

        <div className="desktop-status-actions">
          <Button onClick={() => navigate("/dashboard")}>{t("进入总览看板")}</Button>
          <Button variant="secondary" onClick={() => navigate("/desktop/help")}>{t("查看桌面使用指南")}</Button>
          <Button variant="secondary" onClick={() => navigate("/system-config")}>{t("查看系统配置")}</Button>
        </div>
      </Card>

      <div className="desktop-status-grid">
        <Card className="desktop-status-card">
          <span>{t("桌面首跳入口")}</span>
          <strong>/ui/</strong>
          <p>{t("桌面壳先进入 /ui/ 根入口，再由前端无刷切到 /ui/desktop 状态页；这样能避开原生壳对首个深路径文档请求的兼容性差异。")}</p>
        </Card>

        <Card className="desktop-status-card">
          <span>{t("认证来源")}</span>
          <strong>window.__RESIN_DESKTOP_BOOTSTRAP__</strong>
          <p>{t("桌面壳会在页面启动前注入 desktop=true 与会话 token，浏览器模式则继续使用原有登录入口。")}</p>
        </Card>

        <Card className="desktop-status-card">
          <span>{t("当前 token 预览")}</span>
          <strong className="desktop-status-code">{maskToken(token)}</strong>
          <p>{t("状态页只展示脱敏片段，避免在界面上暴露 raw token。")}</p>
        </Card>

        <Card className="desktop-status-card">
          <span>{t("持久化策略")}</span>
          <strong>{sessionKind === "desktop" ? t("仅内存，不写 localStorage") : t("浏览器本地持久化")}</strong>
          <p>{t("无论桌面还是浏览器，API 请求模型都继续保持 /api/ + Authorization: Bearer。")}</p>
        </Card>
      </div>

      <div className={`callout ${desktopMode ? "callout-success" : "callout-warning"}`}>
        <span>
          {desktopMode
            ? t("桌面会话已启用：关闭当前窗口不会把 token 落进浏览器持久化存储。")
            : t("当前仍是浏览器行为：如果需要桌面会话注入，请从桌面壳入口打开 /ui/。")}
        </span>
      </div>

      <Card className="desktop-status-hero">
        <h3>{t("后续帮助不再依赖首启页")}</h3>
        <p>
          {t("首启页只负责一次性初始化和启动恢复；以后如果你需要查看代理接入示例、Proxy Token 用法、代理池验证方法或常见排障说明，请直接进入“桌面使用指南”。")}
        </p>
        <div className="desktop-status-actions">
          <Button onClick={() => navigate("/desktop/help")}>{t("打开桌面使用指南")}</Button>
        </div>
      </Card>
    </section>
  );
}
