package tryon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// 上游响应体读取上限，防止恶意/异常上游返回超大响应耗尽内存。
const (
	modelMaxResponseBytes  int64 = 64 << 20 // 生成结果（含 base64 图片的 JSON）
	modelMaxErrorBodyBytes int64 = 4 << 10  // 非 200 响应只读少量内容用于脱敏日志
	visionMaxResponseBytes int64 = 1 << 20  // 视觉描述（纯文本 JSON）
	modelMaxRedirects            = 3
)

var errResponseTooLarge = errors.New("模型服务响应过大")

// forbiddenPrefixes 浏览器自定义模型地址禁止访问的 IANA 特殊用途/非公网地址段。
// 参考 IANA IPv4/IPv6 Special-Purpose Address Registry，并额外覆盖云厂商元数据常用段（100.64/10 内的 100.100.100.200）。
var forbiddenPrefixes = mustPrefixes(
	// IPv4
	"0.0.0.0/8",       // 本网络
	"10.0.0.0/8",      // 私有
	"100.64.0.0/10",   // CGNAT（含阿里云元数据 100.100.100.200）
	"127.0.0.0/8",     // 回环
	"169.254.0.0/16",  // 链路本地（含 169.254.169.254 元数据）
	"172.16.0.0/12",   // 私有
	"192.0.0.0/24",    // IETF 协议分配
	"192.0.2.0/24",    // TEST-NET-1
	"192.88.99.0/24",  // 6to4 中继任播
	"192.168.0.0/16",  // 私有
	"198.18.0.0/15",   // 基准测试（常被用作 fake-IP 网关）
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // 组播
	"240.0.0.0/4",     // 保留（含 255.255.255.255 广播）
	// IPv6
	"::/96",          // 未指定 / 回环 / IPv4 兼容地址
	"::ffff:0:0/96",  // IPv4 映射（Unmap 后再按 IPv4 规则判断，此处兜底）
	"64:ff9b::/96",   // NAT64 well-known
	"64:ff9b:1::/48", // NAT64 本地
	"100::/64",       // 丢弃前缀
	"2001::/23",      // IETF 协议分配（含 Teredo 2001::/32、ORCHID）
	"2001:db8::/32",  // 文档
	"2002::/16",      // 6to4
	"3fff::/20",      // 文档
	"5f00::/16",      // SRv6 SID
	"fc00::/7",       // 唯一本地
	"fe80::/10",      // 链路本地
	"fec0::/10",      // 站点本地（已废弃）
	"ff00::/8",       // 组播
)

func mustPrefixes(items ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		out = append(out, netip.MustParsePrefix(item))
	}
	return out
}

// isForbiddenAddr 判断地址是否属于禁止访问的范围：只允许公网全球单播地址。
func isForbiddenAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	if addr.Zone() != "" {
		return true
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return true
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// isForbiddenClientIP 兼容 net.IP 的包装。
func isForbiddenClientIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return isForbiddenAddr(addr)
}

// clientAllowedPorts 浏览器自定义模型地址允许的端口，默认仅 HTTPS 默认端口 443；
// 启动时可通过 security.clientModelAllowedPorts 调整（SetClientAllowedPorts）。
var clientAllowedPorts atomic.Pointer[map[string]bool]

func init() {
	SetClientAllowedPorts([]int{443})
}

// SetClientAllowedPorts 设置浏览器自定义模型 Base URL 允许的端口；空列表或无效端口时回退为 443。
func SetClientAllowedPorts(ports []int) {
	m := make(map[string]bool, len(ports))
	for _, p := range ports {
		if p >= 1 && p <= 65535 {
			m[strconv.Itoa(p)] = true
		}
	}
	if len(m) == 0 {
		m["443"] = true
	}
	clientAllowedPorts.Store(&m)
}

func currentClientAllowedPorts() map[string]bool {
	return *clientAllowedPorts.Load()
}

// clientAllowedPortsText 返回已允许端口的可读列表（用于错误提示）。
func clientAllowedPortsText() string {
	ports := make([]int, 0)
	for p := range currentClientAllowedPorts() {
		n, _ := strconv.Atoi(p)
		ports = append(ports, n)
	}
	sort.Ints(ports)
	parts := make([]string, len(ports))
	for i, n := range ports {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, "、")
}

func isAllowedClientPort(port string) bool {
	if port == "" {
		return true // 由 scheme 决定（https → 443）
	}
	return currentClientAllowedPorts()[port]
}

// checkModelRedirect 限制重定向：最多 3 次，只允许同协议、同主机、同端口，且禁止 https→http 降级。
// Go 在跨主机重定向时只会剥离 Authorization/Cookie，x-goog-api-key 等自定义鉴权头会被转发，因此必须禁止跨主机。
func checkModelRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= modelMaxRedirects {
		return errors.New("模型服务重定向次数过多")
	}
	orig := via[0].URL
	if req.URL.Scheme != orig.Scheme || !strings.EqualFold(req.URL.Host, orig.Host) {
		return errors.New("模型服务重定向到其他地址，已拒绝")
	}
	return nil
}

// checkDownloadRedirect 结果图片下载的重定向策略：不携带鉴权头，可跨主机（CDN），但只允许 HTTPS。
func checkDownloadRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= modelMaxRedirects {
		return errors.New("结果图片重定向次数过多")
	}
	if req.URL.Scheme != "https" {
		return errors.New("结果图片地址必须使用 HTTPS")
	}
	for _, h := range []string{"Authorization", "X-Goog-Api-Key", "Api-Key", "X-Api-Key", "Cookie"} {
		req.Header.Del(h)
	}
	return nil
}

// newRestrictedTransport 仅能连接公网全球单播地址的 Transport：在拨号阶段解析并校验每个 IP（防 DNS rebinding），并关闭系统代理。
func newRestrictedTransport(allowedPorts map[string]bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 用户可控 URL 不走系统代理，避免代理代为访问内网地址而绕过下面的目标 IP 校验。
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("模型服务地址无效")
		}
		if allowedPorts != nil && !allowedPorts[port] {
			return nil, fmt.Errorf("模型服务端口不允许访问")
		}
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("模型服务域名解析失败")
		}
		foundPublic := false
		for _, addr := range addrs {
			if isForbiddenAddr(addr) {
				continue
			}
			foundPublic = true
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
			if dialErr == nil {
				return conn, nil
			}
		}
		if !foundPublic {
			return nil, fmt.Errorf("模型服务地址不允许访问")
		}
		return nil, fmt.Errorf("无法连接模型服务")
	}
	return transport
}

// newDownloadHTTPClient 用于下载上游返回的结果图片 URL：URL 来自上游响应，统一按不可信处理。
func newDownloadHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       120 * time.Second,
		Transport:     newRestrictedTransport(map[string]bool{"443": true}),
		CheckRedirect: checkDownloadRedirect,
	}
}

// readLimited 读取至多 limit 字节；超过上限时返回 errResponseTooLarge。
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errResponseTooLarge
	}
	return data, nil
}

// readModelResponse 读取上游响应体：先检查 Content-Length，再限量读取；非 200 时只读少量内容。
func readModelResponse(resp *http.Response) ([]byte, error) {
	limit := modelMaxResponseBytes
	if resp.StatusCode != http.StatusOK {
		limit = modelMaxErrorBodyBytes
		data, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
		return data, nil
	}
	if resp.ContentLength > limit {
		return nil, errResponseTooLarge
	}
	return readLimited(resp.Body, limit)
}

// validateResultURL 校验上游返回的结果图片地址：只允许 HTTPS、默认端口、非 IP 字面量内网地址。
func validateResultURL(raw string) error {
	if len(raw) > 4096 {
		return fmt.Errorf("结果图片地址无效")
	}
	u, err := parseHTTPSURL(raw)
	if err != nil {
		return fmt.Errorf("结果图片地址无效")
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("结果图片地址无效")
	}
	return nil
}

// trustedResultOrigin 返回服务端配置模型 Base URL 的 origin（scheme://host:port）。
// 管理员在服务端配置的 Base URL 视为可信（可以是内网或非 443 端口的自建中转），
// 上游返回与之同源的结果图片地址时允许直接下载；浏览器传入的配置一律返回空串。
func trustedResultOrigin(cfg *ModelConfig) string {
	if cfg == nil || cfg.ClientProvided {
		return ""
	}
	return urlOrigin(cfg.BaseURL)
}

// urlOrigin 规范化为 "scheme://host:port"（小写、补全默认端口）；仅支持 http/https 且不含用户信息。
func urlOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch scheme {
	case "https":
		if port == "" {
			port = "443"
		}
	case "http":
		if port == "" {
			port = "80"
		}
	default:
		return ""
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// downloadResultImage 下载上游返回的结果图片：与服务端配置 Base URL 同源的地址走可信客户端，
// 其余地址按不可信处理（仅 HTTPS 公网 443）；均不携带任何鉴权头，并限制大小。
func downloadResultImage(ctx context.Context, rawURL, trustedOrigin string) (string, error) {
	if len(rawURL) > 4096 {
		return "", fmt.Errorf("结果图片地址无效")
	}
	client := downloadHTTPClient
	if trustedOrigin != "" && urlOrigin(rawURL) == trustedOrigin {
		client = trustedDownloadHTTPClient
	} else if err := validateResultURL(rawURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("结果图片地址无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法下载模型结果图片")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("无法下载模型结果图片 (status %d)", resp.StatusCode)
	}
	if resp.ContentLength > modelMaxResponseBytes {
		return "", errResponseTooLarge
	}
	data, err := readLimited(resp.Body, modelMaxResponseBytes)
	if err != nil {
		return "", err
	}
	return encodeB64(data), nil
}

// downloadHTTPClient / trustedDownloadHTTPClient 可在测试中替换。
var (
	downloadHTTPClient        = newDownloadHTTPClient()
	trustedDownloadHTTPClient = &http.Client{Timeout: 120 * time.Second, CheckRedirect: checkModelRedirect}
)
