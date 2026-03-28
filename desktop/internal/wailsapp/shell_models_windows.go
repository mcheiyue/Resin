//go:build windows

package wailsapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Resinat/Resin/desktop/internal/configstore"
	"github.com/Resinat/Resin/desktop/internal/lifecycle"
	"github.com/Resinat/Resin/desktop/internal/supervisor"
)

const (
	ErrorCodeCoreStartFailed        = "CORE_START_FAILED"
	ErrorCodeCoreExitedEarly        = "CORE_EXITED_EARLY"
	ErrorCodeWebView2RuntimeInvalid = "WEBVIEW2_RUNTIME_INVALID"
	ErrorCodeConfigValidationFailed = "CONFIG_VALIDATION_FAILED"
	desktopAuthVersion              = "V1"

	shellConfigFileName            = "shell-config.json"
	shellConfigVersion             = 1
	fixedListenAddress             = "127.0.0.1"
	defaultPort                    = 2260
	fixedRuntimeExecutableRelative = "runtime/webview2-fixed/msedgewebview2.exe"
)

type Bootstrapper func(rootDir string) (*configstore.BootstrapResult, error)

type SupervisorFactory func(*configstore.BootstrapResult, ShellLaunchConfig) (CoreSupervisor, error)

type ShellLaunchConfig struct {
	ListenAddress string
	Port          int
}

type ShellPaths struct {
	RootDir          string
	LogDir           string
	DesktopDir       string
	FixedRuntimePath string
}

type ShellViewModel struct {
	State       lifecycle.State          `json:"state"`
	Wizard      *FirstRunWizardViewModel `json:"wizard,omitempty"`
	Diagnostics *DiagnosticsViewModel    `json:"diagnostics,omitempty"`
	Progress    *ShellProgressViewModel  `json:"progress,omitempty"`
}

type ShellProgressViewModel struct {
	State          lifecycle.State `json:"state"`
	Phase          string          `json:"phase"`
	Summary        string          `json:"summary"`
	Detail         string          `json:"detail,omitempty"`
	ElapsedMs      int64           `json:"elapsedMs,omitempty"`
	PhaseElapsedMs int64           `json:"phaseElapsedMs,omitempty"`
}

type DesktopAccessView struct {
	DesktopMode            bool   `json:"desktopMode"`
	SessionMode            string `json:"sessionMode"`
	ListenAddress          string `json:"listenAddress"`
	Port                   int    `json:"port"`
	WebUIURL               string `json:"webUiUrl"`
	HealthURL              string `json:"healthUrl"`
	ForwardProxyURL        string `json:"forwardProxyUrl"`
	AuthVersion            string `json:"authVersion"`
	AdminTokenSet          bool   `json:"adminTokenSet"`
	ProxyTokenSet          bool   `json:"proxyTokenSet"`
	LogDir                 string `json:"logDir"`
	StateDir               string `json:"stateDir"`
	CacheDir               string `json:"cacheDir"`
	DesktopDir             string `json:"desktopDir"`
	FixedRuntimePath       string `json:"fixedRuntimePath"`
	ProxyForwardExample    string `json:"proxyForwardExample"`
	ProxyReverseExample    string `json:"proxyReverseExample"`
	ProxyHeaderExample     string `json:"proxyHeaderExample"`
	OpenWebUILabel         string `json:"openWebUiLabel"`
	OpenLogsLabel          string `json:"openLogsLabel"`
	CopyDiagnosticsLabel   string `json:"copyDiagnosticsLabel"`
	OpenDashboardLabel     string `json:"openDashboardLabel"`
	OpenSubscriptionsLabel string `json:"openSubscriptionsLabel"`
	OpenPlatformsLabel     string `json:"openPlatformsLabel"`
	OpenNodesLabel         string `json:"openNodesLabel"`
	OpenRequestLogsLabel   string `json:"openRequestLogsLabel"`
	OpenSystemConfigLabel  string `json:"openSystemConfigLabel"`
}

type FirstRunWizardViewModel struct {
	PortableRootDir    string `json:"portableRootDir"`
	ListenAddress      string `json:"listenAddress"`
	Port               int    `json:"port"`
	PortEditable       bool   `json:"portEditable"`
	TokenSummary       string `json:"tokenSummary"`
	ConfirmActionLabel string `json:"confirmActionLabel"`
}

type PortOccupantViewModel struct {
	PID            uint32 `json:"pid"`
	ImageName      string `json:"imageName,omitempty"`
	ExecutablePath string `json:"executablePath,omitempty"`
}

type DiagnosticsViewModel struct {
	Code               string                 `json:"code"`
	Summary            string                 `json:"summary"`
	Details            string                 `json:"details"`
	RootDir            string                 `json:"rootDir"`
	LogDir             string                 `json:"logDir"`
	FixedRuntimePath   string                 `json:"fixedRuntimePath"`
	ListenAddress      string                 `json:"listenAddress"`
	Port               int                    `json:"port"`
	PortEditable       bool                   `json:"portEditable"`
	PortOccupant       *PortOccupantViewModel `json:"portOccupant,omitempty"`
	HealthURL          string                 `json:"healthUrl,omitempty"`
	OpenLogActionLabel string                 `json:"openLogActionLabel"`
	RetryActionLabel   string                 `json:"retryActionLabel"`
	CopyActionLabel    string                 `json:"copyActionLabel"`
	CopyText           string                 `json:"copyText"`
}

type persistedShellConfig struct {
	Version         int    `json:"version"`
	WizardCompleted bool   `json:"wizard_completed"`
	ListenAddress   string `json:"listen_address"`
	Port            int    `json:"port"`
}

func deriveShellPaths(rootDir string, bootstrap *configstore.BootstrapResult) (ShellPaths, error) {
	if bootstrap != nil {
		return ShellPaths{
			RootDir:          bootstrap.Layout.RootDir,
			LogDir:           bootstrap.Layout.LogDir,
			DesktopDir:       bootstrap.Layout.DesktopDir,
			FixedRuntimePath: filepath.Join(bootstrap.Layout.RootDir, filepath.FromSlash(fixedRuntimeExecutableRelative)),
		}, nil
	}

	cleanRoot := strings.TrimSpace(rootDir)
	if cleanRoot == "" {
		return ShellPaths{}, fmt.Errorf("root directory must not be empty")
	}

	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return ShellPaths{}, fmt.Errorf("resolve root directory: %w", err)
	}

	return ShellPaths{
		RootDir:          absRoot,
		LogDir:           filepath.Join(absRoot, filepath.FromSlash("data/logs")),
		DesktopDir:       filepath.Join(absRoot, filepath.FromSlash("data/desktop")),
		FixedRuntimePath: filepath.Join(absRoot, filepath.FromSlash(fixedRuntimeExecutableRelative)),
	}, nil
}

func defaultShellLaunchConfig() ShellLaunchConfig {
	return ShellLaunchConfig{
		ListenAddress: fixedListenAddress,
		Port:          defaultPort,
	}
}

func normalizeShellLaunchConfig(config ShellLaunchConfig) ShellLaunchConfig {
	normalized := config
	if strings.TrimSpace(normalized.ListenAddress) == "" {
		normalized.ListenAddress = fixedListenAddress
	}
	if normalized.Port == 0 {
		normalized.Port = defaultPort
	}
	return normalized
}

func (c ShellLaunchConfig) Validate() error {
	if strings.TrimSpace(c.ListenAddress) != fixedListenAddress {
		return fmt.Errorf("listen address must stay fixed at %s, got %q", fixedListenAddress, c.ListenAddress)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must stay within 1-65535, got %d", c.Port)
	}
	return nil
}

func buildWizardView(paths ShellPaths, launchConfig ShellLaunchConfig) *FirstRunWizardViewModel {
	config := normalizeShellLaunchConfig(launchConfig)
	return &FirstRunWizardViewModel{
		PortableRootDir:    paths.RootDir,
		ListenAddress:      config.ListenAddress,
		Port:               config.Port,
		PortEditable:       true,
		TokenSummary:       "首次启动时会自动生成并本地保存 admin/proxy token；桌面壳只通过 RESIN_* 环境变量注入 Core，不会把 raw token 放进命令行或页面持久化存储。若本地端口已占用，可以先改一个 1-65535 范围内的端口再启动。",
		ConfirmActionLabel: "立即启动 Core",
	}
}

func buildDesktopAccessView(paths ShellPaths, launchConfig ShellLaunchConfig, bootstrap *configstore.BootstrapResult) DesktopAccessView {
	config := normalizeShellLaunchConfig(launchConfig)
	listenEndpoint := fmt.Sprintf("%s:%d", config.ListenAddress, config.Port)
	webUIURL := fmt.Sprintf("http://%s/ui/", listenEndpoint)
	healthURL := fmt.Sprintf("http://%s/healthz", listenEndpoint)
	forwardProxyURL := fmt.Sprintf("http://%s", listenEndpoint)
	adminTokenSet := bootstrap != nil && strings.TrimSpace(bootstrap.Secrets.AdminToken) != ""
	proxyTokenSet := bootstrap != nil && strings.TrimSpace(bootstrap.Secrets.ProxyToken) != ""

	return DesktopAccessView{
		DesktopMode:            true,
		SessionMode:            "memory-only",
		ListenAddress:          config.ListenAddress,
		Port:                   config.Port,
		WebUIURL:               webUIURL,
		HealthURL:              healthURL,
		ForwardProxyURL:        forwardProxyURL,
		AuthVersion:            desktopAuthVersion,
		AdminTokenSet:          adminTokenSet,
		ProxyTokenSet:          proxyTokenSet,
		LogDir:                 paths.LogDir,
		StateDir:               filepath.Join(paths.RootDir, filepath.FromSlash("data/state")),
		CacheDir:               filepath.Join(paths.RootDir, filepath.FromSlash("data/cache")),
		DesktopDir:             paths.DesktopDir,
		FixedRuntimePath:       paths.FixedRuntimePath,
		ProxyForwardExample:    fmt.Sprintf("curl.exe -x %s -U \"Default.user_tom:<PROXY_TOKEN>\" https://api.ipify.org", forwardProxyURL),
		ProxyReverseExample:    fmt.Sprintf("curl.exe \"http://%s/<PROXY_TOKEN>/Default.user_tom/https/api.ipify.org\"", listenEndpoint),
		ProxyHeaderExample:     fmt.Sprintf("curl.exe \"http://%s/<PROXY_TOKEN>/Default/https/api.ipify.org\" -H \"X-Resin-Account: user_tom\"", listenEndpoint),
		OpenWebUILabel:         "打开 Resin WebUI（桌面会话）",
		OpenLogsLabel:          "打开日志目录",
		CopyDiagnosticsLabel:   "复制诊断信息",
		OpenDashboardLabel:     "总览看板",
		OpenSubscriptionsLabel: "订阅管理",
		OpenPlatformsLabel:     "平台管理",
		OpenNodesLabel:         "节点池",
		OpenRequestLogsLabel:   "请求日志",
		OpenSystemConfigLabel:  "系统配置",
	}
}

func buildDiagnosticsView(paths ShellPaths, code string, cause error, healthURL string, launchConfig ShellLaunchConfig, portConflict *supervisor.PortConflict) *DiagnosticsViewModel {
	config := normalizeShellLaunchConfig(launchConfig)
	if portConflict != nil {
		config = normalizeShellLaunchConfig(ShellLaunchConfig{
			ListenAddress: portConflict.ListenAddress,
			Port:          portConflict.Port,
		})
	}
	details := ""
	if cause != nil {
		details = cause.Error()
	}
	if healthURL == "" {
		healthURL = fmt.Sprintf("http://%s:%d/healthz", config.ListenAddress, config.Port)
	}

	var occupantView *PortOccupantViewModel
	if portConflict != nil && portConflict.Occupant != nil {
		occupantView = &PortOccupantViewModel{
			PID:            portConflict.Occupant.PID,
			ImageName:      portConflict.Occupant.ImageName,
			ExecutablePath: portConflict.Occupant.ExecutablePath,
		}
	}

	occupantLines := []string{}
	if occupantView != nil {
		occupantLines = append(occupantLines,
			fmt.Sprintf("占用进程 PID: %d", occupantView.PID),
		)
		if occupantView.ImageName != "" {
			occupantLines = append(occupantLines, fmt.Sprintf("占用进程名称: %s", occupantView.ImageName))
		}
		if occupantView.ExecutablePath != "" {
			occupantLines = append(occupantLines, fmt.Sprintf("占用进程路径: %s", occupantView.ExecutablePath))
		}
	}

	copyParts := []string{
		fmt.Sprintf("错误码: %s", code),
		fmt.Sprintf("摘要: %s", diagnosticSummary(code, config, occupantView)),
		fmt.Sprintf("便携目录: %s", paths.RootDir),
		fmt.Sprintf("日志目录: %s", paths.LogDir),
		fmt.Sprintf("固定 Runtime: %s", paths.FixedRuntimePath),
		fmt.Sprintf("健康检查: %s", healthURL),
		fmt.Sprintf("监听地址: %s", config.ListenAddress),
		fmt.Sprintf("监听端口: %d", config.Port),
		fmt.Sprintf("详情: %s", details),
	}
	copyParts = append(copyParts, occupantLines...)
	copyText := strings.TrimSpace(strings.Join(copyParts, "\n"))

	return &DiagnosticsViewModel{
		Code:               code,
		Summary:            diagnosticSummary(code, config, occupantView),
		Details:            details,
		RootDir:            paths.RootDir,
		LogDir:             paths.LogDir,
		FixedRuntimePath:   paths.FixedRuntimePath,
		ListenAddress:      config.ListenAddress,
		Port:               config.Port,
		PortEditable:       true,
		PortOccupant:       occupantView,
		HealthURL:          healthURL,
		OpenLogActionLabel: "打开日志目录",
		RetryActionLabel:   "重试启动",
		CopyActionLabel:    "复制诊断信息",
		CopyText:           copyText,
	}
}

func diagnosticSummary(code string, launchConfig ShellLaunchConfig, occupant *PortOccupantViewModel) string {
	config := normalizeShellLaunchConfig(launchConfig)
	switch code {
	case configstore.ErrorCodeConfigRootNotWritable:
		return "当前便携目录不可写，桌面壳无法创建或更新固定 data/* 布局。"
	case ErrorCodeCoreStartFailed:
		return "桌面壳拉起 Core 时遇到未分类的启动错误，请根据详情和日志目录继续排查。"
	case ErrorCodeConfigValidationFailed:
		return "桌面壳检测到启动配置非法或损坏，需要先修复配置后才能继续启动。"
	case ErrorCodeWebView2RuntimeInvalid:
		return "包内固定 WebView2 runtime 缺失或损坏，当前桌面壳无法继续工作。"
	case ErrorCodeCoreExitedEarly:
		return "Core 在通过 /healthz 就绪前就退出了。"
	case "PORT_IN_USE":
		if occupant != nil && occupant.ImageName != "" {
			return fmt.Sprintf("本地监听地址 %s:%d 已被 %s（PID %d）占用。", config.ListenAddress, config.Port, occupant.ImageName, occupant.PID)
		}
		return fmt.Sprintf("本地监听地址 %s:%d 已被其他进程占用。", config.ListenAddress, config.Port)
	default:
		return "桌面壳启动失败，请查看诊断信息并重试。"
	}
}

func shellConfigPath(paths ShellPaths) string {
	return filepath.Join(paths.DesktopDir, shellConfigFileName)
}

func loadShellConfig(paths ShellPaths) (*persistedShellConfig, bool, error) {
	payload, err := os.ReadFile(shellConfigPath(paths))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read shell config: %w", err)
	}

	var cfg persistedShellConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return nil, true, fmt.Errorf("decode shell config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, true, err
	}
	return &cfg, true, nil
}

func saveShellConfig(paths ShellPaths, launchConfig ShellLaunchConfig, wizardCompleted bool) error {
	config := normalizeShellLaunchConfig(launchConfig)
	if err := config.Validate(); err != nil {
		return err
	}
	cfg := persistedShellConfig{
		Version:         shellConfigVersion,
		WizardCompleted: wizardCompleted,
		ListenAddress:   config.ListenAddress,
		Port:            config.Port,
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal shell config: %w", err)
	}
	if err := os.MkdirAll(paths.DesktopDir, 0o755); err != nil {
		return fmt.Errorf("ensure desktop config directory: %w", err)
	}
	if err := os.WriteFile(shellConfigPath(paths), payload, 0o600); err != nil {
		return fmt.Errorf("write shell config: %w", err)
	}
	return nil
}

func saveCompletedShellConfig(paths ShellPaths, launchConfig ShellLaunchConfig) error {
	return saveShellConfig(paths, launchConfig, true)
}

func (c persistedShellConfig) validate() error {
	if c.Version != shellConfigVersion {
		return fmt.Errorf("unsupported shell config version %d", c.Version)
	}
	if c.ListenAddress != fixedListenAddress {
		return fmt.Errorf("listen address must stay fixed at %s, got %q", fixedListenAddress, c.ListenAddress)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must stay within 1-65535, got %d", c.Port)
	}
	return nil
}

func shellLaunchConfigFromPersisted(config persistedShellConfig) ShellLaunchConfig {
	return normalizeShellLaunchConfig(ShellLaunchConfig{
		ListenAddress: config.ListenAddress,
		Port:          config.Port,
	})
}
