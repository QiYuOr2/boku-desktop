//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

// 安装器创建的快捷方式才是应用启动的真实入口：登录启动必须指向快捷方式所指的那个
// 程序，而不是当前恰好在运行的副本，否则快捷方式启动与登录启动会变成两份安装，
// 更新只覆盖其中一份。

// CleanShortcutTarget 去掉快捷方式在目标周围附带的内容：含空格路径外面的引号，
// 以及其后的参数；路径本身不做其它改写。
func CleanShortcutTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, `"`) {
		rest := strings.TrimPrefix(target, `"`)
		if end := strings.Index(rest, `"`); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	// 未加引号的命令行以第一个 " -" 为程序边界，这是快捷方式给无空格路径附加参数的写法。
	if index := strings.Index(target, " -"); index >= 0 {
		target = target[:index]
	}
	return strings.TrimSpace(target)
}

// ShortcutTarget 读取一个 .lnk 文件并返回它指向的程序，读不出时返回 ""。
// 遇到本版本无法理解的快捷方式（文件损坏、指向网络路径、由未知 shell 写入）一律返回 ""，
// 由调用方回退到当前运行的程序，而不是写入一个无从核对的启动项。
func ShortcutTarget(link string) string {
	data, err := os.ReadFile(link)
	if err != nil {
		return ""
	}
	target, err := lnkTarget(data)
	if err != nil {
		return ""
	}
	return target
}

// applicationShortcutPath 查找安装器创建的快捷方式：当前用户或所有用户的开始菜单
// Programs 文件夹或桌面，按人会去找的顺序。
//
// 按应用自身名字查找，而不是在此写死路径：人按安装时的名字找应用，这里也一样。
func applicationShortcutPath() string {
	name := ApplicationName()
	if name == "" {
		return ""
	}
	for _, folder := range shortcutFolders() {
		path := filepath.Join(folder, name+".lnk")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// shortcutFolders 是安装器放快捷方式的四个位置：当前用户与所有用户的
// 开始菜单 Programs 文件夹和桌面。
//
// 这四个路径直接从环境变量拼出，而不是走 shell 的 known-folder API（本版本没有绑定）：
// 这些路径自 Vista 起在每个 Windows 上都一样，系统语言只改变文件夹显示名，不改变路径。
func shortcutFolders() []string {
	candidates := []string{
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("USERPROFILE"), "Desktop"),
		filepath.Join(os.Getenv("PROGRAMDATA"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("PUBLIC"), "Desktop"),
	}
	folders := make([]string, 0, len(candidates))
	for _, folder := range candidates {
		// 环境变量未设置时 filepath.Join 会留下相对路径，从而指到工作目录下的文件夹；
		// 只有绝对路径才是这四个之一。
		if !filepath.IsAbs(folder) {
			continue
		}
		if !folderExists(folder) || containsFolder(folders, folder) {
			continue
		}
		folders = append(folders, folder)
	}
	return folders
}

// folderExists 报告文件夹是否存在。
func folderExists(folder string) bool {
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

// containsFolder 报告文件夹是否已在列表中，按 Windows 习惯忽略大小写：
// 单账户机器上，当前用户与所有用户的桌面是同一个文件夹。
func containsFolder(folders []string, folder string) bool {
	for _, existing := range folders {
		if strings.EqualFold(existing, folder) {
			return true
		}
	}
	return false
}
