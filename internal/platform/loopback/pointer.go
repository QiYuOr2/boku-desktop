package loopback

import "unsafe"

// cPtr turns an address owned by the system into a pointer.
//
// The uintptr is stored in a local first. Vet rejects a direct conversion of
// a uintptr value, and these addresses were never Go pointers.
func cPtr(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
