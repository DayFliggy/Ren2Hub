package service

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

// 校验使用本客户端的策略；DNS 结果由实际直连／代理传输固定，不能先用
// 全局解析器做一次与拨号脱节的查询。代理自身是管理员配置，允许本地地址。
func validateProtectedFetchRequest(req *http.Request, getProtection func() (*common.SSRFProtection, bool, error)) error {
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("invalid fetch URL scheme")
	}
	port := 80
	if req.URL.Scheme == "https" {
		port = 443
	}
	if text := req.URL.Port(); text != "" {
		var err error
		port, err = strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("invalid port: %s", text)
		}
	}
	p, enabled, err := getProtection()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	return p.ValidateNetworkTarget(req.URL.Hostname(), port)
}

func checkPinnedFetchRedirect(_ *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	// http.Client 会对每一跳再次调用受保护的 RoundTrip。
	return nil
}
