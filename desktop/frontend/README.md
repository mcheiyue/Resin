# desktop/frontend

这里保留桌面壳最小前端内容，用于 Wails 窗口壳层展示首启确认页与启动失败诊断页。

- 这里不是 Resin 业务 WebUI。
- 不复制 `webui/dist`、React 业务页面或 `/ui/` 的现有前端资源。
- 这里只承载壳层入口、首启确认、诊断页和最小桌面覆盖层。
- 不在这里实现 Resin 业务 WebUI；实际业务页面仍由 Core 通过 `/ui/` 提供。
