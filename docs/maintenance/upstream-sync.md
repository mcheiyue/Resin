# Resin 桌面版上游同步与审查纪律

## 目的与范围
将桌面化 fork 的变更约束在低冲突、可解释、可回滚的范围内，保证随时可与上游同步。本文适用于所有 PR、同步分支与发布前检查。

## 热点禁触清单（需强制说明与评审）
- `cmd/resin/*`：仅允许为 Windows 信号兼容添加最小 `SIGBREAK` 监听补丁，其他修改默认禁止，必须在 PR 描述中写明必要性与回滚方式。
- `internal/proxy/*`、`internal/subscription/*`、`internal/probe/*`：视为高冲突核心域，除安全修复外一律禁止，确需修改必须附带上游 issue/commit 引用与可选代案。
- 根级配置/监听边界：任何试图改变 `RESIN_*` 启动契约、监听地址（固定 `127.0.0.1:2260`）、或破坏 `/healthz` `/ui/` `/api/` 路由的改动均视为红线。

## 低冲突区（优先落变更）
- `desktop/` 模块、自有脚本、文档与便携打包工序；必须保持 **desktop/ 不得 import 根仓库 `internal/*`** 的检查。
- 维护文档与上游同步说明（本文件、架构契约、README 边界声明）。

## PR 审查闸口
1. **触碰热区即强制评审**：凡涉及 `cmd/resin/*`、`internal/proxy/*`、`internal/subscription/*`、`internal/probe/*`、监听地址/认证模型/单实例协议（named mutex + named pipe）或 WebView2 runtime 策略的改动，PR 描述必须包含：改动原因、上游差异、回滚方式、影响面、验证记录。
2. **接口/契约变更须列出消费方影响**：含 `/healthz`、`/ui/`、`/api/`、`RESIN_*` 环境变量、便携目录布局、单实例行为。
3. **桌面模块隔离检查**：CI/Review 需确认 desktop/ 未引用 `internal/*`，Core 依旧以子进程与环境变量契约运行。
4. **安全底线**：不得引入令牌泄漏路径（命令行、URL、浏览器存储、日志），不得移除 WebView2 fixed runtime 依赖或单实例保护。

## 分支与同步策略
- 维护 `upstream/main`（镜像上游）、`origin/main`（fork 基线）和短生命周期 feature 分支。同步流程固定为：`fetch upstream` → 在同步专用分支 `sync/<date>` 上 `merge --ff-only upstream/main`（或 rebase feature onto 更新后的 main）→ 解决冲突后经 PR 合并回 `origin/main`，避免直接在 main 上手工杂糅。
- 每个 PR 保持窄范围：优先拆分为“桌面壳/文档/打包”单一主题，禁止在同一 PR 同时修改桌面壳与 `cmd/resin/*`。
- 若上游变更触及热点目录，先把上游提交原样同步进 fork，再在独立提交里添加桌面侧兼容层；禁止在同一提交混合上游同步与本地改动。

## 何时允许触碰 `cmd/resin/*`
- 唯一预先批准的例外：为 Windows `CTRL_BREAK_EVENT` 兼容补充 `SIGBREAK` 监听，以便托盘“退出”路径能优雅关停 Core。
- 其他改动需满足：存在阻断级缺陷且已在 PR 描述中列明上游 issue/复现路径；提供最小化补丁与回滚方案；通过 full regression（含单实例、监听端口、WebView2 fixed runtime 及 token 安全检查）。

## 上游同步与回归验证要求
- 同步或热点改动的 PR 必须附带：桌面壳单实例回归（named mutex / named pipe）、Core 启动监听 `127.0.0.1:2260`、便携目录可写检查、WebView2 fixed runtime 在包内存在的证据。
- 任何对认证、进程模型或数据目录的更改，必须补充对应文档（架构契约或本文件）并更新验收脚本/检查清单。
