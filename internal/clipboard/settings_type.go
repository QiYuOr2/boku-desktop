// 本文件是剪贴板工具自己的配置：只关乎剪贴板的开关。
//
// 它随功能而居而非放进 internal/domain，因为只有剪贴板会读它。功能存储的东西
// 应当像那个功能本身，只被一个功能使用的类型属于那个功能——domain 是留给多个
// 功能共享的实体与端口的。
//
// 它仍写入应用唯一的设置文件，所用的键仍是存储设计 §8 描述的那些：分组是值的属性，
// 而非存放位置的属性。internal/settings 拥有那个文件，但并不认识这个类型。
package clipboard

import "time"

// Settings 是用户对剪贴板工具所作的选择：是否显示该栏、是否后台读取、
// 多久读一次、保留多少历史。
//
// 两个开关默认关闭。剪贴板是 Boku 能读到的最敏感的东西，因此读取需用户主动开启，
// 而非需要用户去找并关闭。
type Settings struct {
	// RegionVisible 在快捷窗口中显示剪贴板栏。
	RegionVisible bool
	// Monitoring 后台读取剪贴板。关闭即完全不读，而非隐藏地读。
	Monitoring bool
	// PollInterval 监控时读取剪贴板的间隔。低于 MinPollInterval 的值会被抬高到它。
	PollInterval time.Duration
	// HistorySize 是历史保留的最近复制条数，也决定历史页面列出多少条。
	// 不是 HistorySizes 之一的值会被替换为 DefaultHistorySize。
	HistorySize int
}

// Normalize 填补存储设置缺失的部分，并把间隔带回边界内。它从不失败：
// 手工编辑过的文件不应妨碍应用启动，只需被修正到合理值。
func (s Settings) Normalize() Settings {
	if s.PollInterval < MinPollInterval {
		s.PollInterval = DefaultPollInterval
	}
	if !ValidHistorySize(s.HistorySize) {
		s.HistorySize = DefaultHistorySize
	}
	return s
}

// Interval 是边界内的轮询间隔。
func (s Settings) Interval() time.Duration {
	if s.PollInterval < MinPollInterval {
		return DefaultPollInterval
	}
	return s.PollInterval
}

// KeepsHistory 是历史保留的复制条数，不论存储值为何都为某个记录数量范围。
func (s Settings) KeepsHistory() int {
	if !ValidHistorySize(s.HistorySize) {
		return DefaultHistorySize
	}
	return s.HistorySize
}

// Reads 报告是否读取剪贴板，快捷窗口的剪贴板栏依赖于此。
func (s Settings) Reads() bool { return s.RegionVisible && s.Monitoring }

// 剪贴板轮询的边界。下限防止错误配置把轮询变成忙循环；默认值取自工具设计 §4.1。
const (
	MinPollInterval     = 200 * time.Millisecond
	DefaultPollInterval = 700 * time.Millisecond
	// MaxTextBytes 是单次复制保留的文本量上限，更长的会被截断：
	// 持有整个文件的剪贴板不能整个进入历史。
	MaxTextBytes = 64 << 10
	// MaxImageBytes 是读取剪贴板位图的大小上限。更大的不读取：条目仍标明
	// 复制了一张图片，只是应用不持有它。
	MaxImageBytes = 16 << 20
)

// 用户可选的记录数量范围，以条目计：短列表便于扫一眼，或更长给常回头翻的人。
// 它是在三档中选一而非填一个数，因为没人能理解的边界等于没人会设的边界。
const (
	MinHistorySize     = 10
	DefaultHistorySize = 30
	MaxHistorySize     = 50
)

// historySizes 是按提供顺序排列的选项。用数组使其不可更改，
// 也使「是否为其一」的判断在帧上无需分配。
var historySizes = [...]int{MinHistorySize, DefaultHistorySize, MaxHistorySize}

// HistorySizes 返回用户可选的记录数量范围，从短到长。
func HistorySizes() []int { return append([]int(nil), historySizes[:]...) }

// ValidHistorySize 报告 n 是否为某个记录数量范围。
func ValidHistorySize(n int) bool {
	for _, size := range historySizes {
		if size == n {
			return true
		}
	}
	return false
}
