// Package atomicfile 以配置文件应有的方式写文件：先在同目录写临时文件，再改名到目标位置。
//
// 因此读取方只会看到旧内容或新内容，不会看到写了一半的文件；写入中途崩溃也不会
// 丢失用户原有数据。
//
// 本包不持有状态，也不知道写的是什么：使用它的存储会在自己包裹的错误里标明来源。
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// FileMode 是写出文件请求的权限。Windows 上访问控制由 ACL 决定，
	// 这个权限位只是请求而非安全边界。
	FileMode = 0o600
	DirMode  = 0o700
)

// Write 把 data 写到 path，目录不存在时一并创建。
//
// 临时文件建在目标同目录，而不是系统临时目录：跨文件系统的改名实际是复制，
// 而复制正是本包要避免的半写文件。
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer func() {
		if name != "" {
			_ = os.Remove(name)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := temp.Chmod(FileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("restrict %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("move %s into place: %w", name, err)
	}
	name = ""
	return nil
}
