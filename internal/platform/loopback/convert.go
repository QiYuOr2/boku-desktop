// Package loopback captures the default playback or microphone device and
// hands the recognizer 16 kHz mono s16le. The conversion is separate from the
// device so it can be tested without one.
package loopback

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	// TargetRate is what CrispASR reads from stdin.
	TargetRate = 16000
	// FormatFloat32 is interleaved little-endian IEEE float samples.
	FormatFloat32 = "f32"
	// FormatS16 is interleaved little-endian signed 16-bit samples.
	FormatS16 = "s16"
)

// PCM is interleaved audio in one of the two formats a device actually gives.
type PCM struct {
	Format     string
	SampleRate int
	Channels   int
	Data       []byte
}

// ToS16Mono16k mixes channels and resamples to 16 kHz mono s16le.
func ToS16Mono16k(in PCM) ([]byte, error) {
	if in.SampleRate <= 0 || in.Channels <= 0 {
		return nil, fmt.Errorf("loopback: sample rate %d channels %d", in.SampleRate, in.Channels)
	}
	frameBytes, err := frameBytes(in.Format, in.Channels)
	if err != nil {
		return nil, err
	}
	frames := len(in.Data) / frameBytes
	if frames == 0 {
		return nil, nil
	}
	mono := make([]float64, frames)
	for frame := 0; frame < frames; frame++ {
		base := frame * frameBytes
		var sum float64
		for channel := 0; channel < in.Channels; channel++ {
			sum += sampleAt(in.Format, in.Data[base:], channel)
		}
		mono[frame] = sum / float64(in.Channels)
	}
	outFrames := frames
	if in.SampleRate != TargetRate {
		outFrames = int(math.Round(float64(frames) * float64(TargetRate) / float64(in.SampleRate)))
		if outFrames < 1 {
			outFrames = 1
		}
	}
	out := make([]byte, outFrames*2)
	if in.SampleRate == TargetRate {
		for i, sample := range mono {
			binary.LittleEndian.PutUint16(out[i*2:], floatToS16(sample))
		}
		return out, nil
	}
	scale := float64(in.SampleRate) / float64(TargetRate)
	last := float64(frames - 1)
	for i := 0; i < outFrames; i++ {
		position := float64(i) * scale
		if position > last {
			position = last
		}
		left := int(position)
		frac := position - float64(left)
		sample := mono[left]
		if left+1 < frames {
			sample = mono[left]*(1-frac) + mono[left+1]*frac
		}
		binary.LittleEndian.PutUint16(out[i*2:], floatToS16(sample))
	}
	return out, nil
}

func frameBytes(format string, channels int) (int, error) {
	switch format {
	case FormatFloat32:
		return 4 * channels, nil
	case FormatS16:
		return 2 * channels, nil
	default:
		return 0, fmt.Errorf("loopback: unknown sample format %q", format)
	}
}

func sampleAt(format string, frame []byte, channel int) float64 {
	switch format {
	case FormatFloat32:
		bits := binary.LittleEndian.Uint32(frame[channel*4:])
		return float64(math.Float32frombits(bits))
	case FormatS16:
		return float64(int16(binary.LittleEndian.Uint16(frame[channel*2:]))) / 32768
	default:
		return 0
	}
}

func floatToS16(sample float64) uint16 {
	if sample > 1 {
		sample = 1
	}
	if sample < -1 {
		sample = -1
	}
	scaled := math.Round(sample * 32767)
	return uint16(int16(scaled))
}

// ErrUnsupported is a platform this build does not capture on.
var ErrUnsupported = errors.New("loopback: this platform has no capture")

// ErrPermission is a capture the user has not allowed.
var ErrPermission = errors.New("loopback: permission denied")

// DeniedError says which permission is missing and where it is turned on.
type DeniedError struct {
	Kind        string
	SettingsURL string
}

func (e *DeniedError) Error() string {
	if e.Kind == "microphone" {
		return "需要麦克风权限"
	}
	return "需要屏幕与系统音频录制权限"
}

func (e *DeniedError) Unwrap() error { return ErrPermission }
