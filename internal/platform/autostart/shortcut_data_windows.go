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

// A .lnk file holds where a shortcut points. Only one part of it is read
// here: the LinkInfo block, whose LocalBasePath and CommonPathSuffix together
// are the target's path. The others — the ID list, the description, the
// environment-variable block — answer questions this does not ask.
//
// The format is [MS-SHLLINK]. It is read rather than parsed by the shell's own
// objects: reaching IShellLinkW from this CGO-free build means calling a COM
// method through its vtable by hand, and on a Go thread that call is not
// reliably safe. A reader that is wrong here returns "" and the caller falls
// back to the running program; the shell objects would crash the process.
//
// Every read is bounds-checked and every failure is "not a shortcut this
// build understands", never a panic: a .lnk is a file on the user's disk that
// anything can write.

const (
	// lnkHeaderSize is the fixed part of the shell link header, up to and
	// including the file-size field.
	lnkHeaderSize = 0x4C
	// lnkHasLinkTargetIDList and lnkHasLinkInfo are the two flag bits the
	// walk to LinkInfo needs.
	lnkHasLinkTargetIDList = 0x00000001
	lnkHasLinkInfo         = 0x00000002
	// linkInfoHeaderSize is the minimum size of the LinkInfo block's own
	// header: its size, its header size, its flags and its five offsets.
	linkInfoHeaderSize = 0x1C
)

// errNotAShortcut is a file that is not a shortcut this build can read: the
// caller treats it the same as "there is no shortcut".
var errNotAShortcut = errors.New("autostart: the file is not a shortcut")

// lnkTarget returns the program a shortcut points at: LocalBasePath followed
// by CommonPathSuffix, which is how the format splits a path the two ways a
// shortcut can be moved.
func lnkTarget(data []byte) (string, error) {
	if len(data) < lnkHeaderSize || binary.LittleEndian.Uint32(data[0:]) != lnkHeaderSize {
		return "", errNotAShortcut
	}
	flags := binary.LittleEndian.Uint32(data[20:])
	if flags&lnkHasLinkInfo == 0 {
		// A shortcut without LinkInfo has no path that can be read here: it
		// may hold only an ID list, which names a program the way the shell
		// finds it rather than by path.
		return "", errNotAShortcut
	}
	offset := lnkHeaderSize
	if flags&lnkHasLinkTargetIDList != 0 {
		// The ID list is preceded by its own two-byte length.
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
	// The six header fields: HeaderSize, Flags, VolumeIDOffset,
	// LocalBasePathOffset, CommonNetworkRelativeLinkOffset,
	// CommonPathSuffixOffset.
	baseOffset := binary.LittleEndian.Uint32(linkInfo[16:])
	suffixOffset := binary.LittleEndian.Uint32(linkInfo[24:])

	// Both paths are read the same way. Which encoding they are in is not
	// taken from the block's VolumeIDAndLocalBasePath flag: Windows writes
	// that flag for the volume fields and still writes the two paths in the
	// system's code page, which is what an explorer-created shortcut on a
	// Chinese Windows holds. The encoding is recognised from the bytes
	// instead — see lnkString.
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
	// The suffix is the part of the path that the shortcut's own directory
	// does not supply; the two are written one after the other.
	return base + suffix, nil
}

// lnkString reads one NUL-terminated string at offset. An offset of 0 is the
// format's "not present", which is an empty string rather than an error.
//
// The string is either the system's ANSI code page or UTF-16, and the format
// does not say which in a field this build can trust. So it is recognised: a
// UTF-16 path written on a Western or Chinese Windows is full of zero bytes
// between the ASCII letters, and an ANSI path has none there. The first two
// cases — a letter with a zero after it, and a zero with a letter after it —
// only happen in one of the two encodings in this position.
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

// decodeUTF16 reads a NUL-terminated UTF-16 string. A trailing odd byte
// cannot be part of one: it means the terminator fell off the end, which is
// the same string.
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

// indexByte finds the first zero byte, or -1.
func indexByte(data []byte, want byte) int {
	for index, value := range data {
		if value == want {
			return index
		}
	}
	return -1
}

// decodeANSI decodes the system's ANSI code page by way of the Windows API:
// a shortcut written on a Chinese Windows has Chinese in this field, and this
// build has no table of code pages to decode it with. A failure returns the
// bytes as they are rather than dropping the path — a garbled target still
// names a program the user can see, and the caller only compares it.
func decodeANSI(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// MultiByteToWideChar(CP_ACP = 0, no flags, ...): the bytes are decoded the
	// way Windows itself would decode them.
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

// trimLnkPath removes the whitespace the shell sometimes leaves around the
// two halves it writes, so that the comparison with a real path is not
// defeated by one space.
func trimLnkPath(path string) string { return strings.TrimSpace(path) }

// multiByteToWideChar is the Windows API that decodes a path written in the
// system's ANSI code page.
var multiByteToWideChar = syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// pointerBytes is a pointer to the first byte, or 0 for an empty slice.
func pointerBytes(data []byte) unsafe.Pointer {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Pointer(&data[0])
}
