package gamepad

import (
	"context"
	"time"
)

// RefreshEvery is how stale a DualSense battery may be. Plugging a controller
// in or out is delivered by the system, not by this timer.
const RefreshEvery = 30 * time.Second

// settleEvery collapses the several HID interfaces one controller announces
// into a single look.
const settleEvery = 300 * time.Millisecond

// Watch reports the connected controllers on its own goroutine, first
// immediately and then when one is plugged or unplugged. While a DualSense
// is connected, its battery is read again at every, which is at least
// RefreshEvery. Without device notifications the same interval is the only
// look. changed is not called on the caller's goroutine.
func Watch(ctx context.Context, every time.Duration, changed func([]Pad)) {
	if ctx == nil || changed == nil {
		return
	}
	if every < RefreshEvery {
		every = RefreshEvery
	}
	go watch(ctx, every, changed)
}

func watch(ctx context.Context, every time.Duration, changed func([]Pad)) {
	wake := make(chan struct{}, 1)
	stop, notified := listenHID(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	if stop != nil {
		defer stop()
	}
	last := publish(nil, changed, true)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	settle := time.NewTimer(settleEvery)
	if !settle.Stop() {
		select {
		case <-settle.C:
		default:
		}
	}
	defer settle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			if !settle.Stop() {
				select {
				case <-settle.C:
				default:
				}
			}
			settle.Reset(settleEvery)
		case <-settle.C:
			last = publish(last, changed, false)
		case <-ticker.C:
			if notified && !needsBattery(last) {
				continue
			}
			last = publish(last, changed, false)
		}
	}
}

func publish(last []Pad, changed func([]Pad), force bool) []Pad {
	next := Connected()
	if !force && padsEqual(last, next) {
		return last
	}
	last = append([]Pad(nil), next...)
	changed(append([]Pad(nil), last...))
	return last
}

// needsBattery is true when a later look might change a charge reading
// without a controller being plugged in. A DualShock 4 shares this kind and
// has no battery byte; the extra look is the same slow one.
func needsBattery(pads []Pad) bool {
	for _, pad := range pads {
		if pad.Kind == KindDualSense || pad.Kind == KindSwitchPro || pad.Battery.Known {
			return true
		}
	}
	return false
}
