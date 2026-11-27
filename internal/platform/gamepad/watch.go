package gamepad

import (
	"context"
	"time"
)

// RefreshEvery 是 DualSense 电量允许的最大陈旧时间。手柄插拔由系统通知，
// 不靠此定时器。
const RefreshEvery = 30 * time.Second

// settleEvery 把一个控制器宣告的多个 HID 接口合并为一次探测。
const settleEvery = 300 * time.Millisecond

// Watch 在自己的 goroutine 上报已连接的控制器，先立即上报一次，之后在插拔
// 时上报。若有 DualSense 连接，每个 every（至少 RefreshEvery）重新读取一次
// 电量；无设备通知时同样的间隔是唯一探测。changed 不在调用方 goroutine 上
// 调用。
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

// needsBattery 表示在没有手柄插拔时，后续探测仍可能改变电量读数。
// DualShock 4 同属此类且没有电量字节，额外探测同样是慢速的那次。
func needsBattery(pads []Pad) bool {
	for _, pad := range pads {
		if pad.Kind == KindDualSense || pad.Kind == KindSwitchPro || pad.Battery.Known {
			return true
		}
	}
	return false
}
