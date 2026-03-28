# Resin Windows 桌面化 V1 架构契约

## 适用范围
本契约锁定 V1 Windows-first 桌面版的架构边界，覆盖三层结构、生命周期归属、单实例约束、网络与数据布局、运行时依赖、认证安全与非目标。任何实现、测试与 PR 必须以此为约束，不得弱化。

## 固定三层结构
1. **桌面壳层（desktop/）**：独立 Go 模块（自有 go.mod），负责单实例、窗口/托盘、子进程监督、配置与打包入口。
2. **Resin Core 子进程**：继续使用根仓库 `./cmd/resin` 构建的可执行文件，通过进程边界与 localhost HTTP 交互。
3. **本地 WebUI/API**：由 Core 暴露 `/ui/`、`/api/`、`/healthz`，桌面窗口仅承载显示与最小桌面态注入，不替代 HTTP API。

## 生命周期与单实例契约
- 桌面壳为唯一主进程，Core 始终作为子进程由壳层以 `RESIN_*` 环境变量启动，禁止改造成嵌入式库模式。
- 单实例使用 **Windows named mutex + named pipe 控制通道**：主实例持有 `Local\\ResinDesktopSingleton`，监听 `\\.\\pipe\\resin-desktop-control`；第二次启动只能触发 `REATTACH`/`BLOCKED`，不得并行跑第二个 Core。
- 关闭主窗口 → `HIDE_TO_TRAY`；显式“退出”才触发关停序列：向 Core 发送 `CTRL_BREAK_EVENT`，等待优雅退出，必要时允许一个最小的 `SIGBREAK` 兼容补丁以保持 Windows 信号一致性。
- 壳层异常退出后重启必须先探测既存 Core（/healthz + env 指纹），决定 `REATTACH_CORE` 或 `RECOVER_AND_RESTART`，严禁盲目拉起第二个 Core。

## 网络与监听边界
- Core 仅监听 **`127.0.0.1:2260`**，端口冲突必须阻断启动并给出可机器断言的错误码。
- Web/HTTP 契约沿用上游：`/healthz`、`/ui/`、`/api/` 全部由 Core 提供，桌面壳不得劫持或重写。

## 便携数据与运行时布局
- 便携模式固定相对目录（相对桌面壳 exe 根）：`data/state/`、`data/cache/`、`data/logs/`、`data/desktop/`；任一不可写即阻断启动并提示“当前解压目录不可写”。
- 不写入全局用户目录或注册表，ZIP 解压即用，删除目录即卸载。

## WebView2 策略
- 桌面发行固定为双产物：
  - `full`：包内必须携带 **WebView2 fixed runtime**，作为默认且正式支持的桌面交付物。
  - `lite`：包内不携带 fixed runtime，仅面向系统已具备可用 WebView2 runtime 的场景，属于便利型产物，不作为默认支持路径。
- Release 流程必须对 `full` 执行桌面 smoke；`lite` 至少要完成与其定位匹配的结构校验，避免将 full / lite 的验证口径混为一谈。

## 认证与 token 安全底线
- `RESIN_ADMIN_TOKEN` / `RESIN_PROXY_TOKEN` 由桌面壳生成、持有并通过环境变量注入 Core；禁止出现在命令行、URL、浏览器持久化存储或普通日志。
- WebView 注入仅可在内存态暴露 session token；raw 管理 token 不得落入 `localStorage`。

## 边界与禁触清单
- **模块隔离**：`desktop/` 自有模块，**不得 import 根仓库 `internal/*`**；与 Core 交互仅限进程与 HTTP 契约。
- **进程模型**：Resin Core 必须保持子进程模式，通过 `RESIN_*` 环境变量启动、配置与关停；桌面壳不得改写 Core 监听模型。
- **兼容性补丁**：仅允许为 Windows 信号映射添加最小 `SIGBREAK` 监听补丁；其他对 `cmd/resin/*` 的修改需走同步纪律审查。

## V1 非目标（不得承诺或实现）
- 不做安装器、自动更新或 Windows 服务模式。
- 不做跨平台桌面体验对齐；V1 仅支持 Windows 便携 ZIP。
- 不重写 `platforms / subscriptions / nodes` 主流程，不深改代理内核或上游高冲突目录。
- 不承诺随意变更监听端口、认证模型或 WebUI 路由基座。
