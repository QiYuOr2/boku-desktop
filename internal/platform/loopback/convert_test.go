package loopback

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestS16Mono16kPassesThrough(t *testing.T) {
	in := PCM{Format: FormatS16, SampleRate: TargetRate, Channels: 1, Data: []byte{0x00, 0x10, 0xff, 0xff}}
	got, err := ToS16Mono16k(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(in.Data) {
		t.Fatalf("got %x", got)
	}
}

func TestStereoIsMixedBeforeScaling(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint16(data[0:], uint16(int16(1000)))
	binary.LittleEndian.PutUint16(data[2:], uint16(int16(3000)))
	got, err := ToS16Mono16k(PCM{Format: FormatS16, SampleRate: TargetRate, Channels: 2, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	sample := int16(binary.LittleEndian.Uint16(got))
	if sample != 2000 {
		t.Fatalf("sample %d", sample)
	}
}

func TestFloatIsClippedToS16(t *testing.T) {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], math.Float32bits(0.5))
	binary.LittleEndian.PutUint32(data[4:], math.Float32bits(2))
	got, err := ToS16Mono16k(PCM{Format: FormatFloat32, SampleRate: TargetRate, Channels: 1, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	first := int16(binary.LittleEndian.Uint16(got[0:]))
	second := int16(binary.LittleEndian.Uint16(got[2:]))
	if first != 16384 || second != 32767 {
		t.Fatalf("samples %d %d", first, second)
	}
}

func TestResample48kTo16kKeepsTheLength(t *testing.T) {
	frames := 480
	data := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(int16(1000)))
	}
	got, err := ToS16Mono16k(PCM{Format: FormatS16, SampleRate: 48000, Channels: 1, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 160*2 {
		t.Fatalf("bytes %d", len(got))
	}
	sample := int16(binary.LittleEndian.Uint16(got))
	if sample != 1000 {
		t.Fatalf("sample %d", sample)
	}
}

func TestAPartialFrameIsDropped(t *testing.T) {
	got, err := ToS16Mono16k(PCM{Format: FormatS16, SampleRate: TargetRate, Channels: 1, Data: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestUnknownFormatIsRejected(t *testing.T) {
	if _, err := ToS16Mono16k(PCM{Format: "s32", SampleRate: TargetRate, Channels: 1, Data: []byte{1, 2, 3, 4}}); err == nil {
		t.Fatal("s32 was accepted")
	}
}
