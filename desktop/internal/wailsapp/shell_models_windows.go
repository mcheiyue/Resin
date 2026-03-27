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
)

const (
	ErrorCodeCoreExitedEarly        = "CORE_EXITED_EARLY"
	ErrorCodeWebView2RuntimeInvalid = "WEBVIEW2_RUNTIME_INVALID"
	ErrorCodeConfigValidationFailed = "CONFIG_VALIDATION_FAILED"

	shellConfigFileName            = "shell-config.json"
	shellConfigVersion             = 1
	fixedListenAddress             = "127.0.0.1"
	fixedPort                      = 2260
	fixedRuntimeExecutableRelative = "runtime/webview2-fixed/msedgewebview2.exe"
)

type Bootstrapper func(rootDir string) (*configstore.BootstrapResult, error)

type SupervisorFactory func(*configstore.BootstrapResult) (CoreSupervisor, error)

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
}

type FirstRunWizardViewModel struct {
	PortableRootDir    string `json:"portableRootDir"`
	ListenAddress      string `json:"listenAddress"`
	Port               int    `json:"port"`
	TokenSummary       string `json:"tokenSummary"`
	ConfirmActionLabel string `json:"confirmActionLabel"`
}

type DiagnosticsViewModel struct {
	Code               string `json:"code"`
	Summary            string `json:"summary"`
	Details            string `json:"details"`
	RootDir            string `json:"rootDir"`
	LogDir             string `json:"logDir"`
	FixedRuntimePath   string `json:"fixedRuntimePath"`
	HealthURL          string `json:"healthUrl,omitempty"`
	OpenLogActionLabel string `json:"openLogActionLabel"`
	RetryActionLabel   string `json:"retryActionLabel"`
	CopyActionLabel    string `json:"copyActionLabel"`
	CopyText           string `json:"copyText"`
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

func buildWizardView(paths ShellPaths) *FirstRunWizardViewModel {
	return &FirstRunWizardViewModel{
		PortableRootDir:    paths.RootDir,
		ListenAddress:      fixedListenAddress,
		Port:               fixedPort,
		TokenSummary:       "首次启动时会自动生成并本地保存 admin/proxy token；桌面壳只通过 RESIN_* 环境变量注入 Core，不会把 raw token 放进命令行或页面持久化存储。",
		ConfirmActionLabel: "立即启动 Core",
	}
}

func buildDiagnosticsView(paths ShellPaths, code string, cause error, healthURL string) *DiagnosticsViewModel {
	details := ""
	if cause != nil {
		details = cause.Error()
	}
	if healthURL == "" {
		healthURL = fmt.Sprintf("http://%s:%d/healthz", fixedListenAddress, fixedPort)
	}

	copyText := strings.TrimSpace(strings.Join([]string{
		fmt.Sprintf("错误码: %s", code),
		fmt.Sprintf("摘要: %s", diagnosticSummary(code)),
		fmt.Sprintf("便携目录: %s", paths.RootDir),
		fmt.Sprintf("日志目录: %s", paths.LogDir),
		fmt.Sprintf("固定 Runtime: %s", paths.FixedRuntimePath),
		fmt.Sprintf("健康检查: %s", healthURL),
		fmt.Sprintf("监听地址: %s", fixedListenAddress),
		fmt.Sprintf("监听端口: %d", fixedPort),
		fmt.Sprintf("详情: %s", details),
	}, "\n"))

	return &DiagnosticsViewModel{
		Code:               code,
		Summary:            diagnosticSummary(code),
		Details:            details,
		RootDir:            paths.RootDir,
		LogDir:             paths.LogDir,
		FixedRuntimePath:   paths.FixedRuntimePath,
		HealthURL:          healthURL,
		OpenLogActionLabel: "打开日志目录",
		RetryActionLabel:   "重试启动",
		CopyActionLabel:    "复制诊断信息",
		CopyText:           copyText,
	}
}

func diagnosticSummary(code string) string {
	switch code {
	case configstore.ErrorCodeConfigRootNotWritable:
		return "当前便携目录不可写，桌面壳无法创建或更新固定 data/* 布局。"
	case ErrorCodeConfigValidationFailed:
		return "桌面壳检测到启动配置非法或损坏，需要先修复配置后才能继续启动。"
	case ErrorCodeWebView2RuntimeInvalid:
		return "包内固定 WebView2 runtime 缺失或损坏，当前桌面壳无法继续工作。"
	case ErrorCodeCoreExitedEarly:
		return "Core 在通过 /healthz 就绪前就退出了。"
	case "PORT_IN_USE":
		return "固定本地监听地址 127.0.0.1:2260 已被其他进程占用。"
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

func saveCompletedShellConfig(paths ShellPaths) error {
	cfg := persistedShellConfig{
		Version:         shellConfigVersion,
		WizardCompleted: true,
		ListenAddress:   fixedListenAddress,
		Port:            fixedPort,
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

func (c persistedShellConfig) validate() error {
	if c.Version != shellConfigVersion {
		return fmt.Errorf("unsupported shell config version %d", c.Version)
	}
	if c.ListenAddress != fixedListenAddress {
		return fmt.Errorf("listen address must stay fixed at %s, got %q", fixedListenAddress, c.ListenAddress)
	}
	if c.Port != fixedPort {
		return fmt.Errorf("port must stay fixed at %d, got %d", fixedPort, c.Port)
	}
	return nil
}
