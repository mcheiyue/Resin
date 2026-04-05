import { useQuery } from "@tanstack/react-query";
import { BookOpenCheck, ClipboardCheck, ExternalLink, KeyRound, LifeBuoy, Waypoints } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { ToastContainer } from "../../components/ui/Toast";
import { useToast } from "../../hooks/useToast";
import { useI18n } from "../../i18n";
import { getDefaultAppPath, getDesktopHelpPath, isDesktopMode } from "../../lib/desktop-bootstrap";
import {
  getDesktopProxyAccessToken,
  openDesktopLogDirectory,
  copyDesktopDiagnostics,
  hasDesktopAppBridge,
} from "../../lib/desktop-bridge";
import { useAuthStore } from "../auth/auth-store";
import { getEnvConfig } from "../systemConfig/api";

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

async function copyText(value: string): Promise<boolean> {
  if (!value.trim() || !navigator.clipboard?.writeText) {
    return false;
  }
  await navigator.clipboard.writeText(value);
  return true;
}

export function DesktopHelpPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { toasts, showToast, dismissToast } = useToast();
  const sessionKind = useAuthStore((state) => state.sessionKind);
  const adminToken = useAuthStore((state) => state.token);
  const desktopMode = isDesktopMode();
  const desktopHomePath = getDefaultAppPath();
  const desktopHelpPath = getDesktopHelpPath();
  const bridgeAvailable = hasDesktopAppBridge();
  const [revealedProxyToken, setRevealedProxyToken] = useState("");

  const envConfigQuery = useQuery({
    queryKey: ["system-config-env", "desktop-help"],
    queryFn: getEnvConfig,
    staleTime: 30_000,
  });

  const envConfig = envConfigQuery.data;
  const listenAddress = envConfig?.listen_address || "127.0.0.1";
  const listenPort = envConfig?.resin_port || 2260;
  const listenEndpoint = `${listenAddress}:${listenPort}`;
  const webUiUrl = `http://${listenEndpoint}/ui/`;
  const healthUrl = `http://${listenEndpoint}/healthz`;
  const forwardProxyUrl = `http://${listenEndpoint}`;
  const forwardExample = `curl.exe -x ${forwardProxyUrl} -U \"Default.user_tom:<PROXY_TOKEN>\" https://api.ipify.org`;
  const reverseExample = `curl.exe \"http://${listenEndpoint}/<PROXY_TOKEN>/Default.user_tom/https/api.ipify.org\"`;
  const headerExample = `curl.exe \"http://${listenEndpoint}/<PROXY_TOKEN>/Default/https/api.example.com/v1/orders\" -H \"X-Resin-Account: user_tom\"`;
  const stickyTtl = envConfig?.default_platform_sticky_ttl || "7d";
  const authModeText = envConfig?.proxy_token_set ? t("已启用 Proxy Token 鉴权") : t("未启用 Proxy Token 鉴权");

  const quickFacts = useMemo(
    () => [
      { label: t("监听地址"), value: listenAddress },
      { label: t("监听端口"), value: String(listenPort) },
      { label: t("健康检查"), value: healthUrl },
      { label: t("代理入口"), value: forwardProxyUrl },
      { label: t("认证版本"), value: "V1" },
      { label: t("代理鉴权状态"), value: authModeText },
    ],
    [authModeText, healthUrl, listenAddress, listenPort, t, forwardProxyUrl],
  );

  const copyOrToast = async (label: string, value: string) => {
    try {
      const ok = await copyText(value);
      if (!ok) {
        showToast("error", `${label} 复制失败，请检查浏览器剪贴板权限`);
        return;
      }
      showToast("success", `${label} 已复制`);
    } catch {
      showToast("error", `${label} 复制失败，请稍后重试`);
    }
  };

  const revealProxyToken = async () => {
    const token = await getDesktopProxyAccessToken();
    if (!token) {
      showToast("error", "当前桌面会话未提供 Proxy Token，请先确认桌面壳已正常启动");
      return;
    }
    setRevealedProxyToken(token);
    await copyOrToast(t("Proxy Token"), token);
  };

  const openLogs = async () => {
    try {
      const ok = await openDesktopLogDirectory();
      if (!ok) {
        showToast("error", "当前环境无法直接打开日志目录，请返回桌面壳诊断页操作");
      }
    } catch {
      showToast("error", "打开日志目录失败，请稍后重试");
    }
  };

  const copyDiagnostics = async () => {
    const diagnostics = await copyDesktopDiagnostics();
    if (!diagnostics) {
      showToast("error", "当前桌面桥接未提供诊断复制能力，请返回桌面壳诊断页操作");
      return;
    }
    await copyOrToast(t("桌面诊断信息"), diagnostics);
  };

  return (
    <section className="desktop-help-page">
      <header className="module-header">
        <div>
          <h2>{t("桌面使用指南")}</h2>
          <p className="module-description">
            {t("这个页面只承载可重复查看的桌面接入与代理池使用说明；一次性首启确认、启动前端口选择和启动失败诊断仍然留在桌面壳中处理。")}
          </p>
        </div>

        <div className="desktop-status-headline">
          <Badge variant={desktopMode ? "info" : "warning"}>{desktopMode ? t("桌面模式") : t("浏览器模式")}</Badge>
          <Badge variant={sessionKind === "desktop" ? "success" : "muted"}>
            {sessionKind === "desktop" ? t("仅内存会话") : t("浏览器会话")}
          </Badge>
        </div>
      </header>

      <Card className="desktop-help-hero">
        <div className="desktop-help-hero-copy">
          <Badge variant="success">{t("桌面入口分流完成")}</Badge>
          <h3>{t("以后不用再回首启页找说明")}</h3>
          <p>
            {t("桌面首启页只负责一次性初始化和故障恢复；本地代理地址、API 调用示例、代理池验证方法和常见排障入口都可以在这里反复查看。")}
          </p>
        </div>
        <div className="desktop-help-actions">
          <Button onClick={() => navigate("/dashboard")}>{t("进入总览看板")}</Button>
          <Button variant="secondary" onClick={() => navigate("/desktop")}>{t("返回桌面状态")}</Button>
          <Button variant="secondary" onClick={() => navigate("/request-logs")}>{t("查看请求日志")}</Button>
        </div>
      </Card>

      <div className="desktop-help-grid">
        <Card className="desktop-help-card">
          <div className="desktop-help-card-head">
            <BookOpenCheck size={18} />
            <h3>{t("本地接入总览")}</h3>
          </div>
          <div className="desktop-help-facts">
            {quickFacts.map((item) => (
              <div key={item.label} className="desktop-help-fact-item">
                <span>{item.label}</span>
                <strong className="desktop-status-code">{item.value}</strong>
              </div>
            ))}
          </div>
          <p className="desktop-help-muted">
            {t("桌面模式下进入 /ui/ 会自动注入当前 Admin 会话；只有你直接在浏览器访问直连地址时，才需要手动输入当前 Admin Token。")}
          </p>
          <p className="desktop-help-muted">
            {bridgeAvailable
              ? t("当前窗口已检测到桌面桥接，可直接复制 Proxy Token、诊断信息或打开日志目录。")
              : t("当前窗口未检测到桌面桥接，只能查看说明文本；若需桌面动作，请从桌面壳重新打开 /ui/。")}
          </p>
          <div className="desktop-help-inline-actions">
            <Button variant="secondary" onClick={() => copyOrToast(t("WebUI 地址"), webUiUrl)}>{t("复制 WebUI 地址")}</Button>
            <Button variant="secondary" onClick={() => copyOrToast(t("代理地址"), forwardProxyUrl)}>{t("复制代理地址")}</Button>
            <Button variant="secondary" onClick={() => copyOrToast(t("健康检查地址"), healthUrl)}>{t("复制健康检查地址")}</Button>
          </div>
        </Card>

        <Card className="desktop-help-card">
          <div className="desktop-help-card-head">
            <KeyRound size={18} />
            <h3>{t("认证与 token 使用")}</h3>
          </div>
          <div className="desktop-help-facts">
            <div className="desktop-help-fact-item">
              <span>{t("当前 Admin Token 预览")}</span>
              <strong className="desktop-status-code">{maskToken(adminToken)}</strong>
            </div>
            <div className="desktop-help-fact-item">
              <span>{t("当前 Proxy Token")}</span>
              <strong className="desktop-status-code">{revealedProxyToken || t("默认隐藏（点击下方按钮后仅在当前窗口显示）")}</strong>
            </div>
          </div>
          <ul className="desktop-help-list">
            <li>{t("Admin Token 只用于管理控制台登录；桌面会话通常自动接管，不需要你手动填写。")}</li>
            <li>{t("Proxy Token 只用于外部客户端接入正向/反向代理，不默认明文展示，也不会写进普通日志或浏览器持久化存储。")}</li>
          </ul>
          <div className="desktop-help-inline-actions">
            <Button variant="secondary" onClick={revealProxyToken}>{t("显示并复制 Proxy Token")}</Button>
            <Button variant="secondary" onClick={copyDiagnostics}>{t("复制桌面诊断")}</Button>
          </div>
        </Card>

        <Card className="desktop-help-card">
          <div className="desktop-help-card-head">
            <ClipboardCheck size={18} />
            <h3>{t("怎么发第一个请求")}</h3>
          </div>
          <p className="desktop-help-muted">{t("下面三条是当前版本最实用的最小接入示例。默认使用 <PROXY_TOKEN> 占位，避免页面长期明文展示凭据。")}</p>
          <pre>{forwardExample}</pre>
          <pre>{reverseExample}</pre>
          <pre>{headerExample}</pre>
          <div className="desktop-help-inline-actions">
            <Button variant="secondary" onClick={() => copyOrToast(t("正向代理示例"), forwardExample)}>{t("复制正向代理示例")}</Button>
            <Button variant="secondary" onClick={() => copyOrToast(t("反向代理示例"), reverseExample)}>{t("复制反向代理示例")}</Button>
            <Button variant="secondary" onClick={() => copyOrToast(t("Header 示例"), headerExample)}>{t("复制 Header 示例")}</Button>
          </div>
        </Card>

        <Card className="desktop-help-card">
          <div className="desktop-help-card-head">
            <Waypoints size={18} />
            <h3>{t("代理池怎么验证")}</h3>
          </div>
          <ul className="desktop-help-list">
            <li>{t("同一 Platform.Account（例如 Default.user_tom）连续请求得到同一个出口 IP，通常是在证明账号粘性租约生效，不代表代理池失效。")}</li>
            <li>{t("当前默认 sticky TTL 是 {{ttl}}；如果你想验证池内分发，请换不同账号，如 user_a / user_b / user_c，再观察 egress_ip 与 node_hash。", { ttl: stickyTtl })}</li>
            <li>{t("最可靠的观察路径不是重复打 ipify，而是去请求日志看 account / node_hash / egress_ip，再去平台监控看活跃租约与可路由节点。")}</li>
          </ul>
          <div className="desktop-help-inline-actions">
            <Button variant="secondary" onClick={() => navigate("/request-logs")}>{t("查看请求日志")}</Button>
            <Button variant="secondary" onClick={() => navigate("/platforms")}>{t("查看平台管理")}</Button>
            <Button variant="secondary" onClick={() => navigate("/nodes")}>{t("查看节点池")}</Button>
          </div>
        </Card>

        <Card className="desktop-help-card desktop-help-card-wide">
          <div className="desktop-help-card-head">
            <LifeBuoy size={18} />
            <h3>{t("常见问题与排障路径")}</h3>
          </div>
          <div className="desktop-help-columns">
            <div>
              <h4>{t("为什么回不到首启页？")}</h4>
              <p>{t("首启页是一锤子的一次性初始化向导；确认完成后会写入 wizard_completed，不再重复展示。以后重复查看说明，请回到这个“桌面使用指南”。")}</p>
              <h4>{t("什么时候去桌面壳，什么时候去 WebUI？")}</h4>
              <p>{t("端口冲突、启动失败、日志目录、重试启动这类壳层动作仍在桌面壳；代理接入说明、请求示例、日志观察路径和业务配置入口则在 WebUI 里反复查看。")}</p>
            </div>
            <div>
              <h4>{t("如果诊断页说日志目录是空的？")}</h4>
              <p>{t("这通常意味着失败发生在 Core 真正开始写日志之前；请优先复制诊断信息、修改端口后重试，或者直接打开日志目录确认目录权限。")}</p>
              <div className="desktop-help-inline-actions">
                <Button variant="secondary" onClick={openLogs}>{t("打开日志目录")}</Button>
                <Button variant="secondary" onClick={() => navigate("/system-config")}>
                  <ExternalLink size={14} />
                  {t("查看系统配置")}
                </Button>
                <Button variant="secondary" onClick={() => navigate(desktopHomePath)}>{t("返回桌面状态")}</Button>
              </div>
            </div>
          </div>
        </Card>
      </div>

      <p className="desktop-help-muted desktop-help-route-note">
        {t("桌面首页由桌面桥接默认路由控制，当前默认入口是 {{path}}，帮助页路径是 {{helpPath}}。", {
          path: desktopHomePath,
          helpPath: desktopHelpPath,
        })}
      </p>

      <ToastContainer toasts={toasts} onDismiss={dismissToast} />
    </section>
  );
}
