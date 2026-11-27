package domain

import "time"

// Clock 是应用读取时间的入口，便于测试决定时间。
//
// 它是端口，与其它端口放在一起：有两处需要它——剪贴板历史为记录盖时间戳，
// 翻译工具为答复盖时间戳——而两者都不应为了取得它而去 import 对方。
type Clock interface {
	Now() time.Time
}

// SystemClock 是应用运行所用的时钟：UTC 的墙钟时间，
// 与领域模型 §11 的存储方式一致。
type SystemClock struct{}

// Now 返回 UTC 当前时间。若实现自行取整是出于它自己的原因；
// 本实现不取整。
func (SystemClock) Now() time.Time { return time.Now().UTC() }
