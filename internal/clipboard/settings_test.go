package clipboard

import (
	"testing"
	"time"
)

// 记录数量范围是三档之一，其它任何值——早于该设置的文件，或手工编辑的数字——
// 都取默认值，而非成为不启动的理由。
func TestTheRecordRangeIsBroughtIntoItsBounds(t *testing.T) {
	for _, size := range HistorySizes() {
		if !ValidHistorySize(size) {
			t.Errorf("%d is one of the record ranges but is not valid", size)
		}
		settings := Settings{HistorySize: size}.Normalize()
		if got := settings.HistorySize; got != size {
			t.Errorf("Normalize kept %d as %d, want %d", size, got, size)
		}
		if got := settings.KeepsHistory(); got != size {
			t.Errorf("KeepsHistory of %d = %d", size, got)
		}
	}
	for _, size := range []int{0, -1, 7, 11, 100, 1000} {
		if ValidHistorySize(size) {
			t.Errorf("%d should not be a record range", size)
		}
		settings := Settings{HistorySize: size}.Normalize()
		if got := settings.KeepsHistory(); got != DefaultHistorySize {
			t.Errorf("the record range %d became %d, want the default %d",
				size, got, DefaultHistorySize)
		}
	}

	// 规范化范围不会扰动间隔，后者有自己的规则。
	settings := Settings{PollInterval: time.Second, HistorySize: 7}.Normalize()
	if settings.Interval() != time.Second {
		t.Errorf("Normalize changed the interval to %s", settings.Interval())
	}
}

// 轮询间隔有下限：要求比剪贴板能读的速度还快的文件，会耗尽机器的时间去询问。
func TestThePollIntervalHasAFloor(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, time.Millisecond, MinPollInterval - 1} {
		settings := Settings{PollInterval: interval}.Normalize()
		if got := settings.Interval(); got != DefaultPollInterval {
			t.Errorf("an interval of %s became %s, want the default %s", interval, got, DefaultPollInterval)
		}
	}
	settings := Settings{PollInterval: 3 * time.Second}.Normalize()
	if got := settings.Interval(); got != 3*time.Second {
		t.Errorf("an interval inside the bounds became %s", got)
	}
}

// Reads 决定是否读取剪贴板：栏被绘制且其要绘制的内容正在采集时才读，
// 任一个开关关闭即不读。
func TestReadsNeedsBothSwitches(t *testing.T) {
	cases := []struct {
		region, monitor bool
		want            bool
	}{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{true, true, true},
	}
	for _, c := range cases {
		settings := Settings{RegionVisible: c.region, Monitoring: c.monitor}
		if got := settings.Reads(); got != c.want {
			t.Errorf("Reads with region=%v monitoring=%v = %v, want %v",
				c.region, c.monitor, got, c.want)
		}
	}
}

// 全新安装不读取任何内容：剪贴板属于用户，读取前先询问正是开关的意义所在。
func TestAFreshInstallationReadsNothing(t *testing.T) {
	settings := Settings{}.Normalize()
	if settings.Reads() {
		t.Error("a fresh installation starts reading the clipboard")
	}
	if settings.Monitoring {
		t.Error("a fresh installation starts monitoring")
	}
}
