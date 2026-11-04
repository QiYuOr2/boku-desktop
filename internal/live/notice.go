package live

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"

	"boku/internal/platform/httpc"
)

// downloadNotice is the settings line for a failed download. The pinned file
// name stays; local paths and URLs do not.
func downloadNotice(err error) string {
	if err == nil {
		return ""
	}
	reason := downloadReason(err)
	var named *fileError
	if errors.As(err, &named) && named.name != "" {
		return named.name + "：" + reason
	}
	return reason
}

func downloadReason(err error) string {
	var named *fileError
	if errors.As(err, &named) && named.err != nil {
		err = named.err
	}
	switch {
	case errors.Is(err, errChecksum):
		return "文件校验失败"
	case errors.Is(err, errUnsupported):
		return errUnsupported.Error()
	}
	var status *httpc.StatusError
	if errors.As(err, &status) {
		return "服务器返回 " + strconv.Itoa(status.Code)
	}
	if errors.Is(err, syscall.ENOSPC) || containsAny(err, "no space") {
		return "磁盘空间不足"
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || containsAny(err, "permission denied", "access is denied") {
		return "没有写入权限"
	}
	if containsAny(err, "proxy") {
		return "代理连接失败"
	}
	if errors.Is(err, syscall.ETIMEDOUT) || timedOut(err) || containsAny(err, "timeout", "timed out") {
		return "下载超时"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) || containsAny(err, "no such host") {
		return "找不到服务器"
	}
	if containsAny(err, "x509", "certificate", "tls:") {
		return "证书校验失败"
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.ErrUnexpectedEOF) || containsAny(err, "connection reset", "forcibly closed", "connection aborted", "broken pipe", "unexpected eof") {
		return "连接中断"
	}
	if errors.Is(err, syscall.ECONNREFUSED) || containsAny(err, "connection refused", "actively refused") {
		return "连接被拒绝"
	}
	if detail := visibleDetail(err.Error()); detail != "" {
		return detail
	}
	return "下载没有完成"
}

func timedOut(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func containsAny(err error, parts ...string) bool {
	text := strings.ToLower(err.Error())
	for _, part := range parts {
		if strings.Contains(text, part) {
			return true
		}
	}
	return false
}

func visibleDetail(text string) string {
	var kept []string
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, `"'.,;`)
		if field == "" || strings.Contains(field, "://") || looksLikePath(field) {
			continue
		}
		kept = append(kept, field)
	}
	text = strings.Join(kept, " ")
	const max = 80
	runes := []rune(text)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return text
}

func looksLikePath(field string) bool {
	if strings.Contains(field, `\`) || strings.HasPrefix(field, "/") {
		return true
	}
	return len(field) >= 3 && field[1] == ':' && (field[2] == '\\' || field[2] == '/')
}
