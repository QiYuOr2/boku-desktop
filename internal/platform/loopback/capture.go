package loopback

import (
	"context"
	"sync"
)

const (
	// SourceSystem is the default playback device.
	SourceSystem = "system"
	// SourceMicrophone is the default input device.
	SourceMicrophone = "microphone"
)

// Stream is one capture. PCM sends 16 kHz mono s16le chunks. Close stops the
// device. A full queue drops the chunk: the system callback has to return.
type Stream struct {
	mu      sync.Mutex
	pcm     chan []byte
	cancel  context.CancelFunc
	stopped chan struct{}
	closed  bool
}

// PCM is the converted audio. It is closed after the capture stops.
func (s *Stream) PCM() <-chan []byte {
	if s == nil {
		return nil
	}
	return s.pcm
}

// Close stops the capture and waits until the device has been released.
func (s *Stream) Close() error {
	if s == nil || s.cancel == nil {
		return nil
	}
	s.cancel()
	<-s.stopped
	return nil
}

func newStream(parent context.Context) (*Stream, context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Stream{
		pcm:     make(chan []byte, 16),
		cancel:  cancel,
		stopped: make(chan struct{}),
	}, ctx
}

func (s *Stream) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.stopped)
	close(s.pcm)
}

func (s *Stream) emit(ctx context.Context, chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case <-ctx.Done():
	case s.pcm <- append([]byte(nil), chunk...):
	default:
	}
}
