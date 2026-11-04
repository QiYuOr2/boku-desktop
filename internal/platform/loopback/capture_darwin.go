//go:build darwin

package loopback

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"structs"

	"boku/internal/platform/screenrecording"
)

const (
	audioFormatLinearPCM = 0x6c70636d
	audioFlagFloat       = 1 << 0
	audioFlagSigned      = 1 << 2
	audioFlagPacked      = 1 << 3
	streamOutputAudio    = 1
	authAuthorized       = 3
	authNotDetermined    = 0
)

type audioFormat struct {
	_               structs.HostLayout
	SampleRate      float64
	FormatID        uint32
	FormatFlags     uint32
	BytesPerPacket  uint32
	FramesPerPacket uint32
	BytesPerFrame   uint32
	Channels        uint32
	Bits            uint32
	Reserved        uint32
}

var (
	bridgeOnce  sync.Once
	bridgeErr   error
	outputClass objc.Class
	handlers    sync.Map

	cmGetList func(sample uintptr, needed *uintptr, list unsafe.Pointer, listSize uintptr, a, b uintptr, flags uint32, block *uintptr) int32
	cmGetDesc func(sample uintptr) uintptr
	cmGetASBD func(desc uintptr) *audioFormat
	cfRelease func(uintptr)

	aqNewInput func(format *audioFormat, callback uintptr, user unsafe.Pointer, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
	aqAllocate func(queue uintptr, size uint32, buffer *uintptr) int32
	aqEnqueue  func(queue, buffer uintptr, packets uint32, descs uintptr) int32
	aqStart    func(queue, startTime uintptr) int32
	aqStop     func(queue uintptr, immediate bool) int32
	aqDispose  func(queue uintptr, immediate bool) int32

	mediaAudio objc.ID
)

func loadBridge() {
	bridgeOnce.Do(func() {
		foundation, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bridgeErr = err
			return
		}
		_ = foundation
		coreMedia, err := purego.Dlopen("/System/Library/Frameworks/CoreMedia.framework/CoreMedia", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bridgeErr = err
			return
		}
		audio, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bridgeErr = err
			return
		}
		av, err := purego.Dlopen("/System/Library/Frameworks/AVFoundation.framework/AVFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bridgeErr = err
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/ScreenCaptureKit.framework/ScreenCaptureKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			bridgeErr = err
			return
		}
		purego.RegisterLibFunc(&cmGetList, coreMedia, "CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer")
		purego.RegisterLibFunc(&cmGetDesc, coreMedia, "CMSampleBufferGetFormatDescription")
		purego.RegisterLibFunc(&cmGetASBD, coreMedia, "CMAudioFormatDescriptionGetStreamBasicDescription")
		coreFoundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bridgeErr = err
			return
		}
		purego.RegisterLibFunc(&cfRelease, coreFoundation, "CFRelease")
		purego.RegisterLibFunc(&aqNewInput, audio, "AudioQueueNewInput")
		purego.RegisterLibFunc(&aqAllocate, audio, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&aqEnqueue, audio, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&aqStart, audio, "AudioQueueStart")
		purego.RegisterLibFunc(&aqStop, audio, "AudioQueueStop")
		purego.RegisterLibFunc(&aqDispose, audio, "AudioQueueDispose")
		symbol, err := purego.Dlsym(av, "AVMediaTypeAudio")
		if err != nil {
			bridgeErr = err
			return
		}
		mediaAudio = *(*objc.ID)(cPtr(symbol))
		protocol := objc.GetProtocol("SCStreamOutput")
		var protocols []*objc.Protocol
		if protocol != nil {
			protocols = []*objc.Protocol{protocol}
		}
		outputClass, err = objc.RegisterClass(
			"BokuLiveAudioOutput",
			objc.GetClass("NSObject"),
			protocols,
			nil,
			[]objc.MethodDef{{
				Cmd: objc.RegisterName("stream:didOutputSampleBuffer:ofType:"),
				Fn:  didOutputSample,
			}},
		)
		if err != nil {
			bridgeErr = err
		}
	})
}

func Start(ctx context.Context, source string) (*Stream, error) {
	loadBridge()
	if bridgeErr != nil {
		return nil, bridgeErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch source {
	case SourceMicrophone:
		return startMicrophone(ctx)
	case SourceSystem, "":
		return startSystem(ctx)
	default:
		return nil, fmt.Errorf("loopback: unknown source %q", source)
	}
}

func startSystem(ctx context.Context) (*Stream, error) {
	if !screenrecording.Granted() {
		return nil, &DeniedError{Kind: "screen", SettingsURL: screenrecording.SettingsURL()}
	}
	stream, run := newStream(ctx)
	session, delegate, filter, config, err := openSystem(run, stream)
	if err != nil {
		stream.finish()
		return nil, err
	}
	go func() {
		defer stream.finish()
		<-run.Done()
		stopStream(session)
		handlers.Delete(uintptr(delegate))
		releaseID(session)
		releaseID(delegate)
		releaseID(filter)
		releaseID(config)
	}()
	return stream, nil
}

func releaseID(id objc.ID) {
	if id != 0 {
		id.Send(objc.RegisterName("release"))
	}
}

func openSystem(ctx context.Context, stream *Stream) (session, delegate, filter, config objc.ID, err error) {
	content, err := shareableContent(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	displays := content.Send(objc.RegisterName("displays"))
	if displays == 0 || objc.Send[int64](displays, objc.RegisterName("count")) < 1 {
		return 0, 0, 0, 0, errors.New("loopback: no display")
	}
	display := displays.Send(objc.RegisterName("objectAtIndex:"), uintptr(0))
	filter = objc.ID(objc.GetClass("SCContentFilter")).Send(objc.RegisterName("alloc")).Send(
		objc.RegisterName("initWithDisplay:excludingWindows:"), display, objc.ID(0),
	)
	config = objc.ID(objc.GetClass("SCStreamConfiguration")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
	config.Send(objc.RegisterName("setWidth:"), int64(2))
	config.Send(objc.RegisterName("setHeight:"), int64(2))
	config.Send(objc.RegisterName("setCapturesAudio:"), byte(1))
	config.Send(objc.RegisterName("setSampleRate:"), int64(48000))
	config.Send(objc.RegisterName("setChannelCount:"), int64(2))
	config.Send(objc.RegisterName("setExcludesCurrentProcessAudio:"), byte(1))

	delegate = objc.ID(outputClass).Send(objc.RegisterName("new"))
	handlers.Store(uintptr(delegate), stream)
	fail := func(err error) (objc.ID, objc.ID, objc.ID, objc.ID, error) {
		handlers.Delete(uintptr(delegate))
		releaseID(session)
		releaseID(delegate)
		releaseID(filter)
		releaseID(config)
		return 0, 0, 0, 0, err
	}
	session = objc.ID(objc.GetClass("SCStream")).Send(objc.RegisterName("alloc")).Send(
		objc.RegisterName("initWithFilter:configuration:delegate:"), filter, config, delegate,
	)
	if session == 0 {
		return fail(errors.New("loopback: the system audio stream was not created"))
	}
	var addErr objc.ID
	added := objc.Send[bool](session, objc.RegisterName("addStreamOutput:type:sampleHandlerQueue:error:"), delegate, int64(streamOutputAudio), objc.ID(0), &addErr)
	if !added {
		return fail(errors.New("loopback: system audio output was refused"))
	}
	started := make(chan error, 1)
	block := objc.NewBlock(func(_ objc.Block, nserr objc.ID) {
		if nserr == 0 {
			started <- nil
			return
		}
		started <- errors.New(objc.Send[string](nserr.Send(objc.RegisterName("localizedDescription")), objc.RegisterName("UTF8String")))
	})
	defer block.Release()
	session.Send(objc.RegisterName("startCaptureWithCompletionHandler:"), block)
	select {
	case <-ctx.Done():
		stopStream(session)
		return fail(ctx.Err())
	case err := <-started:
		if err != nil {
			return fail(err)
		}
	}
	return session, delegate, filter, config, nil
}

func addrAt(buf []byte, offset int) uintptr {
	return uintptr(binary.LittleEndian.Uint64(buf[offset : offset+8]))
}

func stopStream(session objc.ID) {
	done := make(chan struct{})
	block := objc.NewBlock(func(_ objc.Block, _ objc.ID) { close(done) })
	defer block.Release()
	session.Send(objc.RegisterName("stopCaptureWithCompletionHandler:"), block)
	<-done
}

func shareableContent(ctx context.Context) (objc.ID, error) {
	result := make(chan struct {
		content objc.ID
		err     error
	}, 1)
	block := objc.NewBlock(func(_ objc.Block, content, nserr objc.ID) {
		if nserr != 0 {
			result <- struct {
				content objc.ID
				err     error
			}{err: errors.New(objc.Send[string](nserr.Send(objc.RegisterName("localizedDescription")), objc.RegisterName("UTF8String")))}
			return
		}
		result <- struct {
			content objc.ID
			err     error
		}{content: content}
	})
	defer block.Release()
	objc.ID(objc.GetClass("SCShareableContent")).Send(objc.RegisterName("getShareableContentWithCompletionHandler:"), block)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case got := <-result:
		return got.content, got.err
	}
}

func didOutputSample(self objc.ID, _ objc.SEL, _ objc.ID, sample objc.ID, kind int64) {
	if kind != streamOutputAudio || sample == 0 {
		return
	}
	value, ok := handlers.Load(uintptr(self))
	if !ok {
		return
	}
	stream := value.(*Stream)
	chunk, err := samplePCM(uintptr(sample))
	if err != nil {
		return
	}
	stream.emit(context.Background(), chunk)
}

func samplePCM(sample uintptr) ([]byte, error) {
	desc := cmGetDesc(sample)
	if desc == 0 || cmGetASBD == nil {
		return nil, errors.New("loopback: no audio format")
	}
	format := cmGetASBD(desc)
	if format == nil || format.FormatID != audioFormatLinearPCM {
		return nil, errors.New("loopback: audio is not linear PCM")
	}
	var list [128]byte
	var needed uintptr
	var block uintptr
	status := cmGetList(sample, &needed, unsafe.Pointer(&list[0]), uintptr(len(list)), 0, 0, 1, &block)
	if status != 0 {
		return nil, fmt.Errorf("loopback: audio buffer list %d", status)
	}
	if block != 0 {
		defer cfRelease(block)
	}
	count := binary.LittleEndian.Uint32(list[0:4])
	if count == 0 {
		return nil, nil
	}
	sampleFormat := FormatS16
	if format.FormatFlags&audioFlagFloat != 0 {
		sampleFormat = FormatFloat32
	} else if format.FormatFlags&audioFlagSigned == 0 {
		return nil, errors.New("loopback: unsigned PCM")
	}
	if count == 1 {
		size := binary.LittleEndian.Uint32(list[12:16])
		data := addrAt(list[:], 16)
		if data == 0 || size == 0 {
			return nil, nil
		}
		raw := unsafe.Slice((*byte)(cPtr(data)), size)
		return ToS16Mono16k(PCM{Format: sampleFormat, SampleRate: int(format.SampleRate), Channels: int(format.Channels), Data: raw})
	}
	channels := make([][]byte, 0, count)
	for i := uint32(0); i < count; i++ {
		base := 8 + int(i)*16
		if base+16 > len(list) {
			break
		}
		size := binary.LittleEndian.Uint32(list[base+4 : base+8])
		data := addrAt(list[:], base+8)
		if data == 0 || size == 0 {
			continue
		}
		channels = append(channels, append([]byte(nil), unsafe.Slice((*byte)(cPtr(data)), size)...))
	}
	mixed := interleave(channels, sampleFormat)
	return ToS16Mono16k(PCM{Format: sampleFormat, SampleRate: int(format.SampleRate), Channels: len(channels), Data: mixed})
}

func interleave(channels [][]byte, format string) []byte {
	if len(channels) == 0 {
		return nil
	}
	width := 2
	if format == FormatFloat32 {
		width = 4
	}
	frames := len(channels[0]) / width
	out := make([]byte, frames*width*len(channels))
	for frame := 0; frame < frames; frame++ {
		for channel, data := range channels {
			if (frame+1)*width > len(data) {
				continue
			}
			dst := (frame*len(channels) + channel) * width
			copy(out[dst:dst+width], data[frame*width:(frame+1)*width])
		}
	}
	return out
}

func startMicrophone(ctx context.Context) (*Stream, error) {
	if err := authorizeMicrophone(ctx); err != nil {
		return nil, err
	}
	stream, run := newStream(ctx)
	queue, err := openMicrophone(stream)
	if err != nil {
		stream.finish()
		return nil, err
	}
	go func() {
		defer stream.finish()
		<-run.Done()
		if value, ok := micKeep.Load(queue); ok {
			value.(*micQueue).cancel()
		}
		aqStop(queue, true)
		aqDispose(queue, true)
		micKeep.Delete(queue)
	}()
	return stream, nil
}

func authorizeMicrophone(ctx context.Context) error {
	device := objc.GetClass("AVCaptureDevice")
	status := objc.Send[int64](objc.ID(device), objc.RegisterName("authorizationStatusForMediaType:"), mediaAudio)
	if status == authAuthorized {
		return nil
	}
	if status != authNotDetermined {
		return &DeniedError{Kind: "microphone", SettingsURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"}
	}
	result := make(chan byte, 1)
	block := objc.NewBlock(func(_ objc.Block, granted byte) { result <- granted })
	defer block.Release()
	objc.ID(device).Send(objc.RegisterName("requestAccessForMediaType:completionHandler:"), mediaAudio, block)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case granted := <-result:
		if granted == 0 {
			return &DeniedError{Kind: "microphone", SettingsURL: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"}
		}
		return nil
	}
}

type micQueue struct {
	ctx    context.Context
	cancel context.CancelFunc
	stream *Stream
	format audioFormat
}

func openMicrophone(stream *Stream) (uintptr, error) {
	format := audioFormat{
		SampleRate:      TargetRate,
		FormatID:        audioFormatLinearPCM,
		FormatFlags:     audioFlagSigned | audioFlagPacked,
		BytesPerPacket:  2,
		FramesPerPacket: 1,
		BytesPerFrame:   2,
		Channels:        1,
		Bits:            16,
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := &micQueue{ctx: ctx, cancel: cancel, stream: stream, format: format}
	callback := purego.NewCallback(onMicBuffer)
	var queue uintptr
	if status := aqNewInput(&format, callback, unsafe.Pointer(state), 0, 0, 0, &queue); status != 0 {
		cancel()
		return 0, fmt.Errorf("loopback: microphone %d", status)
	}
	micKeep.Store(queue, state)
	fail := func(err error) (uintptr, error) {
		aqDispose(queue, true)
		micKeep.Delete(queue)
		return 0, err
	}
	for range 3 {
		var buffer uintptr
		if status := aqAllocate(queue, 4096, &buffer); status != 0 {
			return fail(fmt.Errorf("loopback: microphone buffer %d", status))
		}
		if status := aqEnqueue(queue, buffer, 0, 0); status != 0 {
			return fail(fmt.Errorf("loopback: microphone enqueue %d", status))
		}
	}
	if status := aqStart(queue, 0); status != 0 {
		return fail(fmt.Errorf("loopback: microphone start %d", status))
	}
	return queue, nil
}

var micKeep sync.Map

func onMicBuffer(user unsafe.Pointer, queue uintptr, buffer uintptr, _ uintptr, _ uint32, _ uintptr) {
	state, ok := micKeep.Load(queue)
	if !ok || buffer == 0 {
		return
	}
	mic := state.(*micQueue)
	// AudioQueueBuffer puts the pointer before the size, with padding after the
	// leading UInt32, so the size is at 16 and the bytes are at 8.
	size := *(*uint32)(unsafe.Add(cPtr(buffer), 16))
	data := *(*uintptr)(unsafe.Add(cPtr(buffer), 8))
	if size > 0 && data != 0 && mic.ctx.Err() == nil {
		raw := unsafe.Slice((*byte)(cPtr(data)), size)
		chunk, err := ToS16Mono16k(PCM{Format: FormatS16, SampleRate: int(mic.format.SampleRate), Channels: int(mic.format.Channels), Data: raw})
		if err == nil {
			mic.stream.emit(mic.ctx, chunk)
		}
	}
	if mic.ctx.Err() == nil {
		aqEnqueue(queue, buffer, 0, 0)
	}
	_ = user
}
