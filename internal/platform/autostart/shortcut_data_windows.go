//go:build windows

package autostart

import (
	"encoding/binary"
	"errors"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// .lnk 文件记录快捷方式指向何处。这里只读取其中一部分：LinkInfo 块的 LocalBasePath 与 CommonPathSuffix
// 合起来就是目标路径；其余部分（ID 列表、描述、环境变量块）不涉及这里要问的问题。
//
// 格式见 [MS-SHLLINK]。这里自己解析而不用 shell 对象：在 CGO-free 构建中调用 IShellLinkW 需手工通过
// vtable 调 COM 方法，而在 Go 线程上这样做并不可靠（shell 对象会令进程崩溃）。读取失败返回 ""，
// 调用方回退到运行中的程序即可。
//
// 每次读取都做边界检查，任何失败都是"不是本构建能读的快捷方式"，绝不 panic：.lnk 是用户磁盘上
// 任何程序都能写的文件。

const (
	// lnkHeaderSize 是 shell link 头部的固定长度，直到文件大小字段（含）。
	lnkHeaderSize = 0x4C
	// lnkHasLinkTargetIDList 与 lnkHasLinkInfo 是遍历到 LinkInfo 所需的两个标志位。
	lnkHasLinkTargetIDList = 0x00000001
	lnkHasLinkInfo         = 0x00000002
	// linkInfoHeaderSize 是 LinkInfo 块自身头部的最小长度：大小、头大小、标志及五个偏移。
	linkInfoHeaderSize = 0x1C
)

// errNotAShortcut 表示该文件不是本构建能读的快捷方式：调用方把它视同"没有快捷方式"。
var errNotAShortcut = errors.New("autostart: the file is not a shortcut")

// lnkTarget 返回快捷方式指向的程序：LocalBasePath 后接 CommonPathSuffix，即该格式把路径拆成两部分以便
// 快捷方式被移动的方式。
func lnkTarget(data []byte) (string, error) {
	if len(data) < lnkHeaderSize || binary.LittleEndian.Uint32(data[0:]) != lnkHeaderSize {
		return "", errNotAShortcut
	}
	flags := binary.LittleEndian.Uint32(data[20:])
	if flags&lnkHasLinkInfo == 0 {
		// 没有 LinkInfo 的快捷方式在这里读不到路径：它可能只含 ID 列表，后者按 shell 查找程序的方式
		// 而非按路径命名。
		return "", errNotAShortcut
	}
	offset := lnkHeaderSize
	if flags&lnkHasLinkTargetIDList != 0 {
		// ID 列表前自带两字节的长度。
		if offset+2 > len(data) {
			return "", errNotAShortcut
		}
		idListSize := int(binary.LittleEndian.Uint16(data[offset:]))
		offset += 2 + idListSize
	}
	if offset+int(linkInfoHeaderSize) > len(data) {
		return "", errNotAShortcut
	}
	linkInfoSize := int(binary.LittleEndian.Uint32(data[offset:]))
	if linkInfoSize < linkInfoHeaderSize || offset+linkInfoSize > len(data) {
		return "", errNotAShortcut
	}
	linkInfo := data[offset : offset+linkInfoSize]
	headerSize := binary.LittleEndian.Uint32(linkInfo[4:])
	if headerSize < linkInfoHeaderSize || int(headerSize) > len(linkInfo) {
		return "", errNotAShortcut
	}
	// 六个头字段：HeaderSize、Flags、VolumeIDOffset、LocalBasePathOffset、
	// CommonNetworkRelativeLinkOffset、CommonPathSuffixOffset。
	baseOffset := binary.LittleEndian.Uint32(linkInfo[16:])
	suffixOffset := binary.LittleEndian.Uint32(linkInfo[24:])

	// 两条路径以相同方式读取。编码不能依据块中的 VolumeIDAndLocalBasePath 标志判断：Windows 为卷字段写该标志，
	// 却仍以系统代码页写这两条路径（中文 Windows 上资源管理器创建的快捷方式就是如此）。编码改从字节识别，
	// 见 lnkString。
	base, err := lnkString(linkInfo, int(baseOffset))
	if err != nil {
		return "", err
	}
	suffix, err := lnkString(linkInfo, int(suffixOffset))
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", errNotAShortcut
	}
	// 后缀是快捷方式所在目录不提供的那部分路径，两部分一前一后写出。
	return base + suffix, nil
}

// lnkString 读取 offset 处的一个 NUL 结尾字符串。offset 为 0 表示格式中的"不存在"，返回空串而非错误。
//
// 字符串可能是系统 ANSI 代码页或 UTF-16，而格式在此处没有可靠的字段可判断。故按字节识别：
// 西文或中文 Windows 上写出的 UTF-16 路径，ASCII 字母之间满是零字节，ANSI 路径则没有。
// 前两种情况——字母后跟零、零后跟字母——在此位置只会在其中一种编码下出现。
func lnkString(data []byte, offset int) (string, error) {
	if offset == 0 {
		return "", nil
	}
	if offset < 0 || offset > len(data) {
		return "", errNotAShortcut
	}
	rest := data[offset:]
	if len(rest) >= 2 && (rest[1] == 0 || rest[0] == 0) {
		return decodeUTF16(rest), nil
	}
	end := len(rest)
	if index := indexByte(rest, 0); index >= 0 {
		end = index
	}
	return decodeANSI(rest[:end]), nil
}

// decodeUTF16 读取一个 NUL 结尾的 UTF-16 字符串。末尾多出的单个奇数字节不可能属于它：那意味着终止符
// 掉出了末尾，仍是同一字符串。
func decodeUTF16(data []byte) string {
	units := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		unit := binary.LittleEndian.Uint16(data[index:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

// indexByte 查找第一个零字节，找不到返回 -1。
func indexByte(data []byte, want byte) int {
	for index, value := range data {
		if value == want {
			return index
		}
	}
	return -1
}

// decodeANSI 借 Windows API 解码系统 ANSI 代码页：中文 Windows 上写出的快捷方式此字段含中文，
// 而本构建没有代码页表可解码。失败时按原样返回字节而非丢弃路径——乱码目标仍对应一个用户可见的程序，
// 且调用方只做比较。
func decodeANSI(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// MultiByteToWideChar(CP_ACP = 0, 无标志, ...)：按 Windows 自己的方式解码这些字节。
	const (
		cpACP   = 0
		noFlags = 0
	)
	if err := multiByteToWideChar.Find(); err != nil {
		return string(data)
	}
	length, _, _ := multiByteToWideChar.Call(cpACP, noFlags,
		uintptr(pointerBytes(data)), uintptr(int32(len(data))), 0, 0)
	if length == 0 {
		return string(data)
	}
	buffer := make([]uint16, length)
	written, _, _ := multiByteToWideChar.Call(cpACP, noFlags,
		uintptr(pointerBytes(data)), uintptr(int32(len(data))),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(int32(length)))
	if written == 0 {
		return string(data)
	}
	return string(utf16.Decode(buffer[:written]))
}

// trimLnkPath 去掉 shell 有时留在它写出的两部分两端的空白，避免与真实路径比较时被一个空格破坏。
func trimLnkPath(path string) string { return strings.TrimSpace(path) }

// multiByteToWideChar 是解码系统 ANSI 代码页路径所用的 Windows API。
var multiByteToWideChar = syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// pointerBytes 返回指向首个字节的指针，空切片返回 0。
func pointerBytes(data []byte) unsafe.Pointer {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Pointer(&data[0])
}
