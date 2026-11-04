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

// Download writes endpoint to dest.
//
// It is the path a model file takes. GetBytes reads the body into memory and
// stops at 1 MiB, which is smaller than any of these files. An existing dest
// is continued when the server accepts Range; the caller hashes dest after
// this returns. Cancelling ctx leaves whatever has already been written.
func Download(ctx context.Context, endpoint, dest string) error {
	return DownloadProgress(ctx, endpoint, dest, nil)
}

// DownloadProgress is Download, and report sees how many bytes dest holds.
// The count includes bytes already on disk when the server resumes, so a
// continued file does not start at zero. report may be nil. It runs about
// once a megabyte, and once more when the body ends.
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

// StatusError is an HTTP status a download cannot continue from.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("download: status %d", e.Code)
}

// copyDownload writes body onto file, which already holds offset bytes.
// report sees the file's size, not just the bytes of this response.
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

// LocalPort is a free TCP port on 127.0.0.1. The listener is closed before
// this returns; the caller is about to hand the port to another process.
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
