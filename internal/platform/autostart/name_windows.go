//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// ApplicationName 返回应用安装时的名字——开始菜单快捷方式名、Run 键名、任务管理器中人读到的名字。
//
// 它取运行中程序自身去掉扩展名的名字而非常量：开发版叫 "Boku Dev"，它的登录启动不能与已安装应用的
// Run 值共用。
func ApplicationName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := filepath.Base(exe)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
