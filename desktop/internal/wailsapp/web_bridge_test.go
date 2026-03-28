//go:build windows

package wailsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Resinat/Resin/desktop/internal/supervisor"
	wailsassetserver "github.com/wailsapp/wails/v2/pkg/assetserver"
	assetserveroptions "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func TestDesktopWebBridge_UsesInjectedSession(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if err := app.Startup(context.Background()); err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	bridge, err := app.desktopWebBridge()
	if err != nil {
		t.Fatalf("desktopWebBridge() error = %v", err)
	}
	if bridge == nil {
		t.Fatal("desktopWebBridge() returned nil bridge")
	}

	script, err := app.BootstrapScript()
	if err != nil {
		t.Fatalf("BootstrapScript() error = %v", err)
	}
	if !strings.Contains(script, desktopBootstrapJSKey) {
		t.Fatalf("BootstrapScript() = %q, want global key %q", script, desktopBootstrapJSKey)
	}
	if !strings.Contains(script, `"desktop":true`) {
		t.Fatalf("BootstrapScript() = %q, want desktop=true payload", script)
	}
	if !strings.Contains(script, `"token":"`+fx.bootstrap.Secrets.AdminToken+`"`) {
		t.Fatalf("BootstrapScript() = %q, want injected admin session token %q", script, fx.bootstrap.Secrets.AdminToken)
	}
	if strings.Contains(script, fx.bootstrap.Secrets.ProxyToken) {
		t.Fatalf("BootstrapScript() leaked proxy token: %q", script)
	}
	if bridge.Bootstrap().Token != fx.bootstrap.Secrets.AdminToken {
		t.Fatalf("bridge bootstrap token = %q, want %q", bridge.Bootstrap().Token, fx.bootstrap.Secrets.AdminToken)
	}
}

func TestDesktopWebBridge_DesktopStatusRoute(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if err := app.Startup(context.Background()); err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	bridge, err := app.desktopWebBridge()
	if err != nil {
		t.Fatalf("desktopWebBridge() error = %v", err)
	}

	baseRoute, err := app.WebUIBaseRoute()
	if err != nil {
		t.Fatalf("WebUIBaseRoute() error = %v", err)
	}
	if got, want := baseRoute, desktopWebUIBaseRoute; got != want {
		t.Fatalf("WebUIBaseRoute() = %q, want %q", got, want)
	}
	statusRoute, err := app.DesktopStatusRoute()
	if err != nil {
		t.Fatalf("DesktopStatusRoute() error = %v", err)
	}
	if got, want := statusRoute, desktopWebStatusRoute; got != want {
		t.Fatalf("DesktopStatusRoute() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(statusRoute, baseRoute) {
		t.Fatalf("DesktopStatusRoute() = %q, want prefix %q", statusRoute, baseRoute)
	}
	if got := bridge.WebUIBaseRoute(); got != baseRoute {
		t.Fatalf("bridge.WebUIBaseRoute() = %q, want %q", got, baseRoute)
	}
	if got := bridge.DesktopStatusRoute(); got != statusRoute {
		t.Fatalf("bridge.DesktopStatusRoute() = %q, want %q", got, statusRoute)
	}
}

func TestDesktopWebBridge_DesktopAccessViewProvidesSafeLocalAccessInfo(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	accessView := app.DesktopAccessView()
	if !accessView.DesktopMode {
		t.Fatal("DesktopAccessView().DesktopMode = false, want true")
	}
	if got, want := accessView.ListenAddress, fixedListenAddress; got != want {
		t.Fatalf("DesktopAccessView().ListenAddress = %q, want %q", got, want)
	}
	if got, want := accessView.Port, defaultPort; got != want {
		t.Fatalf("DesktopAccessView().Port = %d, want %d", got, want)
	}
	if got, want := accessView.AuthVersion, desktopAuthVersion; got != want {
		t.Fatalf("DesktopAccessView().AuthVersion = %q, want %q", got, want)
	}
	if !accessView.AdminTokenSet || !accessView.ProxyTokenSet {
		t.Fatalf("DesktopAccessView() token posture = admin:%v proxy:%v, want both true", accessView.AdminTokenSet, accessView.ProxyTokenSet)
	}
	if !strings.Contains(accessView.WebUIURL, "/ui/") {
		t.Fatalf("DesktopAccessView().WebUIURL = %q, want /ui/", accessView.WebUIURL)
	}
	if !strings.Contains(accessView.HealthURL, "/healthz") {
		t.Fatalf("DesktopAccessView().HealthURL = %q, want /healthz", accessView.HealthURL)
	}
	if !strings.Contains(accessView.ProxyForwardExample, "<PROXY_TOKEN>") {
		t.Fatalf("DesktopAccessView().ProxyForwardExample = %q, want placeholder token", accessView.ProxyForwardExample)
	}
	for _, value := range []string{
		accessView.ProxyForwardExample,
		accessView.ProxyReverseExample,
		accessView.ProxyHeaderExample,
		accessView.WebUIURL,
		accessView.HealthURL,
	} {
		if strings.Contains(value, fx.bootstrap.Secrets.AdminToken) {
			t.Fatalf("DesktopAccessView leaked admin token in %q", value)
		}
		if strings.Contains(value, fx.bootstrap.Secrets.ProxyToken) {
			t.Fatalf("DesktopAccessView leaked proxy token in %q", value)
		}
	}
	if got, want := accessView.LogDir, fx.bootstrap.Layout.LogDir; got != want {
		t.Fatalf("DesktopAccessView().LogDir = %q, want %q", got, want)
	}
	if got, want := accessView.StateDir, fx.bootstrap.Layout.StateDir; got != want {
		t.Fatalf("DesktopAccessView().StateDir = %q, want %q", got, want)
	}
	if got, want := accessView.CacheDir, fx.bootstrap.Layout.CacheDir; got != want {
		t.Fatalf("DesktopAccessView().CacheDir = %q, want %q", got, want)
	}
	if got, want := accessView.DesktopDir, fx.bootstrap.Layout.DesktopDir; got != want {
		t.Fatalf("DesktopAccessView().DesktopDir = %q, want %q", got, want)
	}
}

func TestDesktopWebBridge_ProxyAccessTokenAvailableForDesktopHub(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if got, want := app.ProxyAccessToken(), fx.bootstrap.Secrets.ProxyToken; got != want {
		t.Fatalf("ProxyAccessToken() = %q, want %q", got, want)
	}
}

func TestDesktopWebBridge_AssetServerMiddlewareInjectsBootstrapIntoDesktopWebUI(t *testing.T) {
	t.Parallel()

	backendHits := make([]string, 0, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		backendHits = append(backendHits, req.URL.Path)
		switch req.URL.Path {
		case "/ui/desktop":
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = rw.Write([]byte("<!doctype html><html><head><title>Resin</title></head><body><div id=\"root\"></div></body></html>"))
		case "/api/ping":
			rw.Header().Set("Content-Type", "application/json")
			_, _ = rw.Write([]byte(`{"ok":true}`))
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	fx := newShellLifecycleFixture(t)
	fx.supervisor.startResult = &supervisor.StartResult{
		Mode:      supervisor.ModeStartedCore,
		PID:       4242,
		HealthURL: backend.URL + "/healthz",
	}
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if err := app.Startup(context.Background()); err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	middleware := app.AssetServerMiddleware()
	handler := middleware(http.NotFoundHandler())

	webuiReq := httptest.NewRequest(http.MethodGet, "http://wails.localhost/ui/desktop", nil)
	webuiRes := httptest.NewRecorder()
	handler.ServeHTTP(webuiRes, webuiReq)

	if got, want := webuiRes.Code, http.StatusOK; got != want {
		t.Fatalf("desktop webui status = %d, want %d", got, want)
	}
	body, err := io.ReadAll(webuiRes.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll(webui body) error = %v", err)
	}
	bodyText := string(body)
	if !strings.Contains(bodyText, desktopBootstrapJSKey) {
		t.Fatalf("desktop webui body missing bootstrap key: %q", bodyText)
	}
	if !strings.Contains(bodyText, fx.bootstrap.Secrets.AdminToken) {
		t.Fatalf("desktop webui body missing injected admin token %q", fx.bootstrap.Secrets.AdminToken)
	}
	if strings.Contains(bodyText, fx.bootstrap.Secrets.ProxyToken) {
		t.Fatalf("desktop webui body leaked proxy token: %q", bodyText)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "http://wails.localhost/api/ping", nil)
	apiRes := httptest.NewRecorder()
	handler.ServeHTTP(apiRes, apiReq)
	if got, want := apiRes.Code, http.StatusOK; got != want {
		t.Fatalf("api proxy status = %d, want %d", got, want)
	}
	if got := strings.TrimSpace(apiRes.Body.String()); got != `{"ok":true}` {
		t.Fatalf("api proxy body = %q, want JSON passthrough", got)
	}
	if got := strings.Join(backendHits, ","); !strings.Contains(got, "/ui/desktop") || !strings.Contains(got, "/api/ping") {
		t.Fatalf("backend hits = %q, want both /ui/desktop and /api/ping", got)
	}
}

func TestDesktopWebBridge_AssetServerMiddlewareFallsBackToWebUIRootForSPARoute(t *testing.T) {
	t.Parallel()

	backendHits := make([]string, 0, 2)
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		backendHits = append(backendHits, req.URL.Path)
		switch req.URL.Path {
		case "/ui/desktop":
			rw.WriteHeader(http.StatusNotFound)
		case "/ui/":
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = rw.Write([]byte("<!doctype html><html><head><title>Resin</title></head><body><div id=\"root\"></div></body></html>"))
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()

	fx := newShellLifecycleFixture(t)
	fx.supervisor.startResult = &supervisor.StartResult{
		Mode:      supervisor.ModeStartedCore,
		PID:       4242,
		HealthURL: backend.URL + "/healthz",
	}
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if err := app.Startup(context.Background()); err != nil {
		t.Fatalf("Startup() error = %v", err)
	}

	middleware := app.AssetServerMiddleware()
	handler := middleware(http.NotFoundHandler())

	webuiReq := httptest.NewRequest(http.MethodGet, "http://wails.localhost/ui/desktop", nil)
	webuiRes := httptest.NewRecorder()
	handler.ServeHTTP(webuiRes, webuiReq)

	if got, want := webuiRes.Code, http.StatusOK; got != want {
		t.Fatalf("desktop webui fallback status = %d, want %d", got, want)
	}
	body, err := io.ReadAll(webuiRes.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll(webui fallback body) error = %v", err)
	}
	bodyText := string(body)
	if !strings.Contains(bodyText, desktopBootstrapJSKey) {
		t.Fatalf("desktop webui fallback body missing bootstrap key: %q", bodyText)
	}
	if got := strings.Join(backendHits, ","); !strings.Contains(got, "/ui/desktop") || !strings.Contains(got, "/ui/") {
		t.Fatalf("backend hits = %q, want both /ui/desktop and /ui/", got)
	}
}

func TestDesktopWebBridge_AssetServerMiddlewareServesDesktopShellWhenProxyUnavailable(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		RootDir:     fx.rootDir,
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	server, err := wailsassetserver.NewAssetServer("", assetserveroptions.Options{
		Assets: fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><head><title>Desktop Shell</title></head><body><div id=\"app\">shell root</div></body></html>")},
		},
		Middleware: app.AssetServerMiddleware(),
	}, false, nil, stubWailsRuntimeAssets{})
	if err != nil {
		t.Fatalf("NewAssetServer() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://wails.localhost/ui/", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if got, want := res.Code, http.StatusOK; got != want {
		t.Fatalf("ui fallback status = %d, want %d", got, want)
	}
	body := res.Body.String()
	if strings.Contains(body, "index.html not found") {
		t.Fatalf("ui fallback body should not expose Wails missing-index page: %q", body)
	}
	if !strings.Contains(body, "Desktop Shell") || !strings.Contains(body, "shell root") {
		t.Fatalf("ui fallback body = %q, want desktop shell html", body)
	}
}

type stubWailsRuntimeAssets struct{}

func (stubWailsRuntimeAssets) DesktopIPC() []byte {
	return []byte("window.__WAILS_IPC__ = true;")
}

func (stubWailsRuntimeAssets) WebsocketIPC() []byte {
	return []byte("window.__WAILS_WS__ = true;")
}

func (stubWailsRuntimeAssets) RuntimeDesktopJS() []byte {
	return []byte("window.__WAILS_RUNTIME__ = true;")
}
