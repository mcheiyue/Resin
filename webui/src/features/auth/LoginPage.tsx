import { zodResolver } from "@hookform/resolvers/zod";
import { LockKeyhole, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { useLocation, useNavigate } from "react-router-dom";
import { z } from "zod";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { Button } from "../../components/ui/Button";
import { LanguageSwitcher } from "../../components/LanguageSwitcher";
import { useAuthStore } from "./auth-store";
import { apiRequest, ApiError } from "../../lib/api-client";
import { useI18n } from "../../i18n";
import { isDesktopMode } from "../../lib/desktop-bootstrap";

const formSchema = z.object({
  token: z.string().trim().min(1, "请输入 Admin Token"),
});

type LoginFormInput = z.infer<typeof formSchema>;

export function LoginPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const setToken = useAuthStore((state) => state.setToken);
  const storedToken = useAuthStore((state) => state.token);
  const [submitError, setSubmitError] = useState("");
  const desktopMode = isDesktopMode();

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormInput>({
    resolver: zodResolver(formSchema),
    defaultValues: { token: "" },
  });

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const next = params.get("next") || "/platforms";

    if (storedToken) {
      navigate(next, { replace: true });
      return;
    }

    let active = true;
    const controller = new AbortController();

    const checkAuthMode = async () => {
      try {
        const response = await fetch("/api/v1/system/info", {
          method: "GET",
          signal: controller.signal,
        });
        if (!active) {
          return;
        }
        if (response.ok) {
          navigate(next, { replace: true });
        }
      } catch {
        // Keep login page for secured deployments or temporary network errors.
      }
    };
    void checkAuthMode();

    return () => {
      active = false;
      controller.abort();
    };
  }, [location.search, navigate, storedToken]);

  const onSubmit = handleSubmit(async (values) => {
    setSubmitError("");

    try {
      await apiRequest("/api/v1/system/info", {
        auth: true,
        token: values.token,
      });
    } catch (error) {
      if (error instanceof ApiError) {
        setSubmitError(t("登录失败：{{message}}", { message: error.message }));
      } else {
        setSubmitError(t("登录失败：无法连接 API。请确认 Resin 在 2260 端口运行，并使用 `npm run dev`（含 /api 代理）启动前端。"));
      }
      return;
    }

    setToken(values.token);

    const params = new URLSearchParams(location.search);
    const next = params.get("next") || "/platforms";
    navigate(next, { replace: true });
  });

  return (
    <main className="login-layout">
      <Card className="login-card">
        <LanguageSwitcher className="login-locale" />

        <div className="login-header">
          <div className="brand-logo" aria-hidden="true">
            <ShieldCheck size={18} />
          </div>
          <div>
            <h1 className="login-title">{t("管理员登录")}</h1>
          </div>
        </div>

        <p className="login-description">
          {desktopMode
            ? t("桌面会话通常会自动接管登录；如果你仍看到这个页面，说明当前桌面会话没有完成注入，可以重新从桌面入口打开 Resin WebUI，或手动输入当前 Admin Token 继续。")
            : t("如果你是直接在浏览器访问本机控制台，请输入当前 Admin Token；如果你已经在桌面版里，请返回桌面入口并点击“打开 Resin WebUI（桌面会话）”。")}
        </p>

        <div className="callout callout-warning">
          <span>
            {desktopMode
              ? t("桌面推荐路径：从桌面壳入口进入 WebUI，无需手动输入 token。")
              : t("浏览器直连只适合手工调试；桌面模式下推荐走桌面会话入口。")}
          </span>
        </div>

        <form className="login-form" onSubmit={onSubmit}>
          <label className="field-label" htmlFor="token">
            {t("当前 Admin Token")}
          </label>
          <div className="input-with-icon">
            <LockKeyhole size={16} />
            <Input
              id="token"
              placeholder={t("粘贴当前 Admin Token（仅本地保存）")}
              autoComplete="off"
              invalid={Boolean(errors.token)}
              {...register("token")}
            />
          </div>

          {errors.token?.message ? <p className="field-error">{t(errors.token.message)}</p> : null}
          {submitError ? <p className="field-error">{submitError}</p> : null}

          <Button type="submit" className="w-full" disabled={isSubmitting}>
            {isSubmitting ? t("校验中...") : t("进入控制台")}
          </Button>
        </form>
      </Card>
    </main>
  );
}
