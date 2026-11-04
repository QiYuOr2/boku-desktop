//go:build windows

package loopback

import (
	"context"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wasapiRender         = 0
	wasapiCapture        = 1
	wasapiConsole        = 0
	clsctxAll            = 23
	streamFlagsLoopback  = 0x00020000
	waveFormatPCM        = 1
	waveFormatFloat      = 3
	waveFormatExtensible = 0xFFFE
)

func Start(ctx context.Context, source string) (*Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	flow := uintptr(wasapiRender)
	flags := uintptr(streamFlagsLoopback)
	switch source {
	case SourceMicrophone:
		flow = wasapiCapture
		flags = 0
	case SourceSystem, "":
	default:
		return nil, fmt.Errorf("loopback: unknown source %q", source)
	}
	stream, run := newStream(ctx)
	started := make(chan error, 1)
	go func() {
		defer stream.finish()
		_ = runCapture(run, stream, flow, flags, started)
	}()
	if err := <-started; err != nil {
		stream.Close()
		return nil, err
	}
	return stream, nil
}

func runCapture(ctx context.Context, stream *Stream, flow, flags uintptr, started chan<- error) error {
	signaled := false
	fail := func(err error) error {
		if !signaled {
			started <- err
			signaled = true
		}
		return err
	}
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		if errno, ok := err.(syscall.Errno); !ok || (errno != 0 && errno != 1) {
			return fail(err)
		}
	}
	defer windows.CoUninitialize()

	enumerator, err := coCreate(clsidEnumerator, iidEnumerator)
	if err != nil {
		return fail(err)
	}
	defer release(enumerator)
	var device uintptr
	if err := hr(call(enumerator, 4, flow, wasapiConsole, uintptr(unsafe.Pointer(&device)))); err != nil {
		return fail(err)
	}
	defer release(device)
	var client uintptr
	if err := hr(call(device, 3, uintptr(unsafe.Pointer(&iidAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))); err != nil {
		return fail(err)
	}
	defer release(client)
	var format *waveFormat
	if err := hr(call(client, 8, uintptr(unsafe.Pointer(&format)))); err != nil {
		return fail(err)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(format))
	sampleFormat, err := sampleFormatOf(format)
	if err != nil {
		return fail(err)
	}
	if err := hr(call(client, 3, 0, flags, 10_000_000, 0, uintptr(unsafe.Pointer(format)), 0)); err != nil {
		return fail(err)
	}
	var capture uintptr
	if err := hr(call(client, 14, uintptr(unsafe.Pointer(&iidCapture)), uintptr(unsafe.Pointer(&capture)))); err != nil {
		return fail(err)
	}
	defer release(capture)
	if err := hr(call(client, 10)); err != nil {
		return fail(err)
	}
	defer call(client, 11)
	started <- nil
	signaled = true

	for ctx.Err() == nil {
		var packets uint32
		if err := hr(call(capture, 5, uintptr(unsafe.Pointer(&packets)))); err != nil {
			return err
		}
		if packets == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		var data uintptr
		var frames, bufFlags uint32
		if err := hr(call(capture, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&bufFlags)), 0, 0)); err != nil {
			return err
		}
		if frames > 0 && data != 0 {
			size := int(frames) * int(format.BlockAlign)
			raw := unsafe.Slice((*byte)(cPtr(data)), size)
			chunk, convErr := ToS16Mono16k(PCM{Format: sampleFormat, SampleRate: int(format.SamplesPerSec), Channels: int(format.Channels), Data: append([]byte(nil), raw...)})
			if convErr == nil {
				stream.emit(ctx, chunk)
			}
		}
		_ = hr(call(capture, 4, uintptr(frames)))
	}
	return nil
}

type waveFormat struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

func sampleFormatOf(format *waveFormat) (string, error) {
	if format == nil {
		return "", fmt.Errorf("loopback: the device has no mix format")
	}
	floatSample := format.FormatTag == waveFormatFloat
	if format.FormatTag == waveFormatExtensible {
		guid := (*windows.GUID)(unsafe.Add(unsafe.Pointer(format), 24))
		floatSample = guid.Data1 == waveFormatFloat
		if guid.Data1 != waveFormatFloat && guid.Data1 != waveFormatPCM {
			return "", fmt.Errorf("loopback: unsupported mix format")
		}
	}
	switch {
	case floatSample && format.BitsPerSample == 32:
		return FormatFloat32, nil
	case !floatSample && format.BitsPerSample == 16:
		return FormatS16, nil
	default:
		return "", fmt.Errorf("loopback: %d-bit audio is not converted", format.BitsPerSample)
	}
}

var (
	clsidEnumerator = guid("BCDE0395-E52F-467C-8E3D-C4579291692E")
	iidEnumerator   = guid("A95664D2-9614-4F35-A746-DE8DB63617E6")
	iidAudioClient  = guid("1CB9AD4C-DBFA-4c32-B178-C2F568A703B2")
	iidCapture      = guid("C8ADBD64-E71E-48a0-A4DE-185C395CD317")
	ole32           = windows.NewLazySystemDLL("ole32.dll")
	coCreateProc    = ole32.NewProc("CoCreateInstance")
)

func guid(value string) windows.GUID {
	parsed, err := windows.GUIDFromString("{" + value + "}")
	if err != nil {
		panic(err)
	}
	return parsed
}

func coCreate(class, iid windows.GUID) (uintptr, error) {
	var obj uintptr
	hrCode, _, _ := coCreateProc.Call(uintptr(unsafe.Pointer(&class)), 0, clsctxAll, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&obj)))
	if err := hr(hrCode, nil); err != nil {
		return 0, err
	}
	return obj, nil
}

func call(obj uintptr, index int, args ...uintptr) (uintptr, error) {
	table := *(**uintptr)(cPtr(obj))
	fn := unsafe.Slice(table, index+1)[index]
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, obj)
	all = append(all, args...)
	result, _, errno := syscall.SyscallN(fn, all...)
	if int32(result) < 0 {
		if errno != 0 {
			return result, errno
		}
		return result, fmt.Errorf("loopback: device call %d failed (%d)", index, int32(result))
	}
	return result, nil
}

func hr(result uintptr, err error) error {
	if err != nil {
		return err
	}
	if int32(result) < 0 {
		return fmt.Errorf("loopback: hresult %d", int32(result))
	}
	return nil
}

func release(obj uintptr) {
	if obj != 0 {
		_, _ = call(obj, 2)
	}
}
