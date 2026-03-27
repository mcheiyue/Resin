<div align="center">
  <img src="webui/public/vite.svg" width="48" alt="Resin Logo" />
  <h1>Resin 桌面版（Windows-first 桌面化 fork）</h1>
  <p><strong>基于 Resinat/Resin Go Core 和内置 WebUI 的 Windows 桌面壳，双击即可跑，本地托盘驻留。</strong></p>
</div>

> 这是一个基于上游 <code>Resinat/Resin</code> 的 Windows-first 桌面化 fork，用于本地运行 Resin Core 与 WebUI。它不是上游官方桌面客户端，也不替代上游的服务端发行版。

## 项目定位

- 在 Windows 上打包 Resin Core 与内置 WebUI，交付一个无需手动配置环境变量、双击即可启动的本地桌面程序。
- 保留上游粘性代理、健康探测、指标日志与 `/ui/` Web 控制台等核心能力，本仓库不额外改动 Core 业务逻辑。
- 桌面壳提供托盘、单实例守护、首启引导与固定 WebView2 运行时，专注“便携 ZIP 解压即用”的桌面体验。

## 与上游关系

- 代码基础来自上游 `Resinat/Resin`，核心仍由 Go 实现，WebUI 继续通过嵌入方式分发（见 `webui/embed.go`，由 Core 对应的 `/ui/` 路由提供入口，见 `internal/api/webui.go`）。
- 保持与上游的功能一致性和配置模型（`internal/config/env.go` 所示的 `RESIN_*` 环境变量仍是 Core 的标准输入），桌面层仅做包装与启动体验，不代表上游官方立场。
- 任何上游核心缺陷或协议兼容性问题，仍建议首先在上游仓库提 Issue；本仓库聚焦桌面打包、托盘与便携发布。

## 保留的 Core 能力

- 粘性代理、平台筛选、主动被动健康探测、请求日志与指标采集等上游核心逻辑完整保留。
- 内置 WebUI 通过 Core 的 `/ui/` 路由对外提供，桌面壳不会覆盖或修改业务 WebUI；前端资产仍由 `webui/dist` 嵌入 Go 二进制。
- 依旧支持 `RESIN_AUTH_VERSION`、`RESIN_ADMIN_TOKEN`、`RESIN_PROXY_TOKEN` 等环境配置，桌面层把这些配置收敛为本地便携目录下的受保护文件，降低首次启动门槛。

## V1 桌面新增能力（Windows-only）

- Wails 桌面壳封装 `resin-core.exe`，附带固定版本的 WebView2 运行时，无需预装浏览器组件。
- 单实例守护与托盘菜单：重复启动只会唤醒已运行实例，托盘提供显示主窗、打开日志目录、显式退出等动作。
- 首次启动引导：在便携目录下创建 `data/state`、`data/cache`、`data/logs`、`data/desktop` 以及受 DPAPI 保护的管理/代理令牌文件，避免手写 `RESIN_*`。
- 桌面前端只承担壳层入口与诊断，业务 WebUI 仍由 Core 在本地 `/ui/` 提供。

## 获取与启动（Windows 便携 ZIP）

1) 在 GitHub Draft Release 中下载桌面双产物：`resinat-windows-amd64-portable.zip` 为 full（包含固定版本 WebView2 runtime，体积更大，解压后双击即用），`resinat-windows-amd64-portable-lite.zip` 为 lite（不含固定 runtime，体积更小，依赖系统已安装 WebView2 runtime）。
2) 如果不确定本机是否已有 WebView2 runtime，或希望离线便携，优先选择 full；已确认系统具备 WebView2 runtime 且希望减小下载体积时可选 lite。
3) 解压到可写目录（不要放在只读路径），保持目录结构完整。
4) 双击 `resinat-desktop.exe`，等待壳层完成首启引导并拉起 Core。
5) 浏览器访问 <http://127.0.0.1:2260/ui/> 打开 Resin WebUI；Core 默认监听本地 2260 端口。
6) 生成的运行数据位于解压目录下的 `data/`，便携移动时请连同整个解压目录一起复制。

## 退出与托盘行为

- 关闭主窗、Alt+F4 或任务栏关闭，只会将窗口隐藏到托盘，Core 继续运行并保持 `/ui/` 与代理入口可用。
- 要彻底停止 Core，需在托盘菜单选择“退出”（显式退出），壳层会优雅关闭 Core 进程后再退出自身。

## 发布产物与渠道

- 桌面发行物固定为双产物：`resinat-windows-amd64-portable.zip`（full，包含固定 WebView2 runtime）与 `resinat-windows-amd64-portable-lite.zip`（lite，不含固定 runtime，需依赖系统已有 WebView2），均由 `.github/workflows/release-desktop.yml` 生成，Release 始终以 Draft 形式发布。
- fork 的 `v*` tag 发布页仅提供上述桌面双产物，不再混出上游服务端相关资产；含 `-` 的 tag 被视为预发布，未带 `-` 的 tag 视为正式版，但都会以 Draft 形式呈现，需从 Draft Release 页面下载。

## 上游同步策略

- 以上游 `Resinat/Resin` 主线为基线，优先跟随上游新 tag，同步时尽量保持 Core 零侵入，仅保留桌面壳与打包脚本所需变更。
- 引入上游更新前会先在 Windows 便携包上进行本地回归（含基本启动与 UI 可用性），再合并到桌面分支。
- 任何与 Core 行为相关的补丁，优先提交到上游或保持可快速 rebase 的最小差异，以降低后续同步成本。

## 非目标

- 不提供 macOS、Linux 或跨平台桌面发行物。
- 不承担上游服务端发行版的发布职责，也不提供官方服务器安装包或 Docker 镜像。
- 不包含额外的第三方节点订阅、加速或未公开的代理配置。

## 贡献与同步流程

1. 先在 Issue 中描述桌面层问题或同步需求，核心协议与性能问题请优先向上游反馈。
2. 贡献代码时保持 Core 零侵入：桌面相关改动集中在 `desktop/`、`scripts/`、`doc/` 等与壳层相关的路径。
3. 同步上游时建议：添加 `upstream` 远程指向 `https://github.com/Resinat/Resin.git`，`git fetch upstream` 后基于上游主线 rebase，再解决桌面层冲突并本地验证便携启动。
4. 提交前请本地运行桌面便携包启动自检，确认 `/ui/` 可访问、托盘显式退出正常，避免引入无法退出或启动失败的行为。

## 许可证与使用范围

- 许可证：MIT，详情见 [LICENSE](LICENSE)。
- 使用范围：仅用于代理调度与管理的技术研究与工程实践，不构成法律、合规、审计或安全建议。
- 合法使用要求：请确保对节点、数据与目标资源的使用具有合法授权，并遵守所在地法律法规及目标服务条款。
- 禁止用途：不得用于未授权访问、规避安全控制、攻击、滥发或其他违法违规行为。
- 免责声明：本项目按“现状”提供，不附带任何明示或默示担保，作者与贡献者不对使用后果承担责任。
