//go:build windows

package wailsapp

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	assetserveroptions "github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"golang.org/x/net/html"
)

func (a *App) AssetServerMiddleware() assetserveroptions.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if a == nil || !shouldProxyDesktopRequest(req.URL.Path) {
				next.ServeHTTP(rw, req)
				return
			}

			handler, err := a.desktopWebProxyHandler(next)
			if err != nil {
				next.ServeHTTP(rw, req)
				return
			}
			handler.ServeHTTP(rw, req)
		})
	}
}

func (a *App) desktopWebProxyHandler(fallback http.Handler) (http.Handler, error) {
	bridge, err := a.desktopWebBridge()
	if err != nil {
		return nil, err
	}
	target, err := a.coreHTTPBaseURL()
	if err != nil {
		return nil, err
	}
	bootstrapScript, err := bridge.BootstrapScript()
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		proxiedReq := req.Clone(req.Context())
		proxiedReq.URL.Scheme = target.Scheme
		proxiedReq.URL.Host = target.Host
		proxiedReq.Host = target.Host
		proxiedReq.RequestURI = ""
		proxiedReq.Header = req.Header.Clone()
		if forwardedHost := req.Host; forwardedHost != "" {
			proxiedReq.Header.Set("X-Forwarded-Host", forwardedHost)
		}

		res, err := http.DefaultClient.Do(proxiedReq)
		if err != nil {
			fallback.ServeHTTP(rw, req)
			return
		}
		defer res.Body.Close()

		body, err := io.ReadAll(res.Body)
		if err != nil {
			fallback.ServeHTTP(rw, req)
			return
		}
		if shouldInjectDesktopBootstrap(res, req.URL.Path) {
			body, err = injectDesktopBootstrap(body, bootstrapScript)
			if err != nil {
				fallback.ServeHTTP(rw, req)
				return
			}
		}

		copyResponseHeaders(rw.Header(), res.Header)
		rw.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		rw.WriteHeader(res.StatusCode)
		_, _ = rw.Write(body)
	}), nil
}

func (a *App) coreHTTPBaseURL() (*url.URL, error) {
	if a == nil || a.lifecycle == nil || a.lifecycle.startResult == nil {
		return nil, fmt.Errorf("core start result is not available")
	}
	healthURL := strings.TrimSpace(a.lifecycle.startResult.HealthURL)
	if healthURL == "" {
		return nil, fmt.Errorf("core health url is not available")
	}
	healthEndpoint, err := url.Parse(healthURL)
	if err != nil {
		return nil, fmt.Errorf("parse core health url: %w", err)
	}
	return &url.URL{Scheme: healthEndpoint.Scheme, Host: healthEndpoint.Host}, nil
}

func shouldProxyDesktopRequest(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") || path == "/ui" || strings.HasPrefix(path, "/ui/")
}

func shouldInjectDesktopBootstrap(res *http.Response, path string) bool {
	if res == nil || res.Request == nil {
		return false
	}
	if res.StatusCode != http.StatusOK {
		return false
	}
	if !strings.HasPrefix(path, "/ui/") && path != "/ui" {
		return false
	}
	return strings.Contains(strings.ToLower(res.Header.Get("Content-Type")), "text/html")
}

func injectDesktopBootstrap(document []byte, bootstrapScript string) ([]byte, error) {
	htmlNode, err := html.Parse(bytes.NewReader(document))
	if err != nil {
		return nil, fmt.Errorf("parse desktop webui html: %w", err)
	}
	headNode := findFirstHTMLTag(htmlNode, "head")
	if headNode == nil {
		return nil, fmt.Errorf("desktop webui html does not contain head")
	}
	scriptNode := &html.Node{
		Type: html.ElementNode,
		Data: "script",
	}
	scriptNode.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: bootstrapScript,
	})
	if headNode.FirstChild != nil {
		headNode.InsertBefore(scriptNode, headNode.FirstChild)
	} else {
		headNode.AppendChild(scriptNode)
	}

	var rendered bytes.Buffer
	if err := html.Render(&rendered, htmlNode); err != nil {
		return nil, fmt.Errorf("render desktop webui html: %w", err)
	}
	return rendered.Bytes(), nil
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key := range dst {
		dst.Del(key)
	}
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func findFirstHTMLTag(node *html.Node, tagName string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && node.Data == tagName {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if result := findFirstHTMLTag(child, tagName); result != nil {
			return result
		}
	}
	return nil
}
