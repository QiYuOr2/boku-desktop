package loopback

import (
	"context"
	"sync"
)

const (
	// 默认播放设备。
	SourceSystem = "system"
	// 默认输入设备。
	SourceMicrophone = "microphone"
)

// Stream 表示一次采集，PCM 输出 16 kHz 单声道 s16le 数据块。队列满时丢弃该块，
// 因为系统回调必须立即返回。Close 会停止设备。
type Stream struct {
	mu      sync.Mutex
	pcm     chan []byte
	cancel  context.CancelFunc
	stopped chan struct{}
	closed  bool
}

// 转换后的音频通道，采集停止后关闭。
func (s *Stream) PCM() <-chan []byte {
	if s == nil {
		return nil
	}
	return s.pcm
}

// 停止采集并等待设备释放完成。
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
