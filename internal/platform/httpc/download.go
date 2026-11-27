package httpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
)

// Download 把 endpoint 写入 dest。
//
// 这是模型文件的下载路径。GetBytes 会把响应读进内存并在 1 MiB 处截断，比这些
// 文件都小。dest 已存在且服务端接受 Range 时断续下载；返回后由调用方对 dest
// 做哈希校验。取消 ctx 会留下已写入的部分。
func Download(ctx context.Context, endpoint, dest string) error {
	return DownloadProgress(ctx, endpoint, dest, nil)
}

// DownloadProgress 同 Download，report 可看到 dest 当前持有的字节数。服务端
// 续传时该计数包含磁盘上已有的字节，因此续传文件不从零开始。report 可为 nil，
// 大约每读写一兆字节调用一次，正文结束时再调一次。
func DownloadProgress(ctx context.Context, endpoint, dest string, report func(written int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", dest, err)
	}
	defer file.Close()

	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("size %s: %w", dest, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build the download: %w", err)
	}
	request.Header.Set("User-Agent", "Boku")
	if offset > 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}

	client := &http.Client{Transport: ProxyTransport()}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		if err := file.Truncate(0); err != nil {
			return fmt.Errorf("truncate %s: %w", dest, err)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("rewind %s: %w", dest, err)
		}
		offset = 0
	case http.StatusPartialContent:
	case http.StatusRequestedRangeNotSatisfiable:
		return nil
	default:
		return &StatusError{Code: response.StatusCode}
	}
	if err := copyDownload(file, response.Body, offset, report); err != nil {
		return err
	}
	return file.Sync()
}

// StatusError 表示下载无法继续的 HTTP 状态码。
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("download: status %d", e.Code)
}

// copyDownload 把 body 写到已持有 offset 字节的 file 上。
// report 看到的是文件大小，而不只是本次响应的字节数。
func copyDownload(file *os.File, body io.Reader, offset int64, report func(int64)) error {
	marked := int64(-1)
	note := func(n int64) {
		if report == nil || n == marked {
			return
		}
		marked = n
		report(n)
	}
	note(offset)
	buf := make([]byte, 1<<20)
	next := offset + int64(len(buf))
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := file.Write(buf[:n]); werr != nil {
				return fmt.Errorf("download: %w", werr)
			}
			offset += int64(n)
			if offset >= next || err != nil {
				note(offset)
				next = offset + int64(len(buf))
			}
		}
		if err == io.EOF {
			note(offset)
			return nil
		}
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
}

// LocalPort 返回 127.0.0.1 上的一个空闲 TCP 端口。返回前监听器已关闭；
// 调用方随即把该端口交给另一个进程。
func LocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}
