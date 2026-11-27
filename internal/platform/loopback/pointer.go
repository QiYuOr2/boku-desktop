package loopback

import "unsafe"

// cPtr 把系统拥有的地址转成指针。
//
// 先把 uintptr 存进局部变量再转换：vet 禁止直接把 uintptr 值转成指针，
// 且这些地址从来不是 Go 指针。
func cPtr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
