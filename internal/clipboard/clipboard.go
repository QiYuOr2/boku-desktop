// Package clipboard 提供剪贴板历史功能：读取平台剪贴板、判定其含义、
// 保留最近若干条记录，并把选中的条目写回剪贴板。
//
// 本文件是服务本体。轮询由 goroutine 驱动，因为平台剪贴板位于主线程，
// 轮询不能占用它。一次读取的含义由纯函数决定，因此可脱离桌面会话测试。
package clipboard

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// maxCopiedTextBytes 限制写回剪贴板的文本大小：从界面复制的是无换行的一行，
// 更大的内容属于误用而非粘贴。
const maxCopiedTextBytes = 1 << 20

// Clipboard 是剪贴板工具：读取平台当前内容、记录为历史、把选中条目写回。
//
// 它是唯一决定用户剪贴板含义的地方，且窗口关闭后仍然存活：历史属于服务而非视图。
// 所有方法都可从任意 goroutine 调用。
type Clipboard struct {
	device  domain.ClipboardDevice
	store   domain.ClipboardStore
	section *Section
	// saver 是保存 section 的存储；为 nil 时设置仅保存在内存中。
	saver    StoreSettings
	now      func() time.Time
	emit     func()
	interval time.Duration
	keep     int

	// watcher 是正在运行的轮询；监控关闭时为 nil。
	watcher *Watcher
	cancel  context.CancelFunc
	done    chan struct{}

	// dirty 表示历史自上次写入后已改变，writing 表示后台写入正在进行。
	// 二者共同保证同一时刻至多一次写入。
	dirty   bool
	writing bool

	mu         sync.Mutex
	latest     *domain.ClipRecord
	history    []domain.ClipboardEntry
	historyErr error
	settings   Settings
	writeAt    time.Time
	writeValue string
	writeKind  domain.ClipKind
	// ignoreUntil 是一段完全不记录剪贴板内容的窗口的结束时间，不论由谁写入。
	// 它用于合成复制——划词工具为读取某个应用不发布的文本而发起的复制
	// （见 IgnoreTheNextCopy）。
	ignoreUntil time.Time
	// lastCopy 是上一次点击历史记录写下的内容，以及写失败时的原因。
	lastCopy    string
	lastCopyErr error
	// readAt 是上一次读取剪贴板的时间。刚打开的窗口可据此判断刚刚读过，
	// 从而请求一帧而不是再读一次。
	readAt time.Time
}

// ClipboardOptions 是装配服务所需的参数。仅 Device 与 Store 必填，其余有默认值。
type ClipboardOptions struct {
	Device domain.ClipboardDevice
	Store  domain.ClipboardStore
	// Section 是本功能的设置（存储持有的形式）：装配时读取，开关变化时经它写入。
	// section 为 nil 时设置仅保存在内存中，适合测试，也适合没有设置文件的装配。
	Section *Section
	// Settings 是保存 section 的存储；为 nil 时设置仅保存在内存中。
	Settings StoreSettings
	Clock    domain.Clock
	Emit     func()
	// Keep 覆盖设置中的记录数量范围。
	Keep int
	// PollIn 覆盖设置中的轮询间隔。
	PollIn time.Duration
}

// NewClipboard 构建服务并读取给定设置。
func NewClipboard(opts ClipboardOptions) (*Clipboard, error) {
	if opts.Device == nil {
		return nil, fmt.Errorf("%w: the clipboard has no device", domain.ErrInvalidArgument)
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("%w: the clipboard has no store", domain.ErrInvalidArgument)
	}
	clock := opts.Clock
	if clock == nil {
		clock = domain.SystemClock{}
	}
	section := opts.Section
	if section == nil {
		section = NewSection()
	}
	clipboard := &Clipboard{
		device:  opts.Device,
		store:   opts.Store,
		section: section,
		saver:   opts.Settings,
		now:     clock.Now,
		emit:    opts.Emit,
		// 选项覆盖设置中的记录数量范围（适合测试）；否则由 load 从设置中填入，
		// 并在读取时按它裁剪已存历史。
		keep: opts.Keep,
	}
	if err := clipboard.load(context.Background()); err != nil {
		return nil, err
	}
	// 两个开关读取时的间隔，就是轮询与循环抑制窗口使用的间隔；
	// 选项可覆盖它（适合测试）。
	clipboard.interval = clipboard.settings.Interval()
	if opts.PollIn >= MinPollInterval {
		clipboard.interval = opts.PollIn
	}
	return clipboard, nil
}

// load 从存储读取设置和历史。
//
// 设置文件读不出是致命错误：它是用户自己的配置，悄悄换一份启动比不启动更糟。
// 历史文件读不出则不然：它只是便利，错误会被记下供测试查看，损坏的历史
// 绝不能连累用户的开关。
//
// 历史在读取时即被裁剪，因此用户在应用未运行期间缩短的记录范围会在展示前生效。
func (c *Clipboard) load(ctx context.Context) error {
	if c.saver != nil {
		if err := c.saver.Load(ctx); err != nil {
			return fmt.Errorf("read the clipboard settings: %w", err)
		}
	}
	c.settings = c.section.Current()
	if c.keep <= 0 {
		c.keep = c.settings.KeepsHistory()
	}
	entries, err := c.store.LoadEntries(ctx)
	if err != nil {
		c.historyErr = err
		entries = nil
	}
	c.history = c.prune(entries)
	return nil
}

// Settings 返回剪贴板自己的开关。
func (c *Clipboard) Settings() Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings
}

// SetEmit 设置内容或历史变化时通知谁，以便界面重绘。装配时接到窗口上；可为 nil。
func (c *Clipboard) SetEmit(emit func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emit = emit
}

// SetRegionVisible 显示或隐藏快捷窗口中的剪贴板栏。
// 隐藏不停止任何行为：用户关掉的只是绘制。
func (c *Clipboard) SetRegionVisible(ctx context.Context, visible bool) error {
	c.mu.Lock()
	c.settings.RegionVisible = visible
	settings := c.settings
	c.mu.Unlock()
	return c.saveSettings(ctx, settings)
}

// SetMonitoring 启停剪贴板读取。停止会结束轮询并等待它返回，
// 使「关闭」意味着不再有读取，而不是读取仍在进行。
func (c *Clipboard) SetMonitoring(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	if c.settings.Monitoring == enabled {
		c.mu.Unlock()
		return nil
	}
	c.settings.Monitoring = enabled
	settings := c.settings
	c.mu.Unlock()
	if err := c.saveSettings(ctx, settings); err != nil {
		return err
	}
	if enabled {
		c.begin(ctx)
		return nil
	}
	c.stop()
	return nil
}

// saveSettings 写入剪贴板自己的开关。
//
// 它走存储的 Update，而非整份设置的 Save：文件持有每个功能的设置，写回本服务
// 持有的副本会把其它功能在此期间改过的值一并复原。Update 会重新读取文件、
// 应用本功能的改动并一次写入，只触碰剪贴板的四个值。
//
// 新开关也会记在本服务中，即使文件写入失败：用户刚拨的开关本次运行已生效。
func (c *Clipboard) saveSettings(ctx context.Context, settings Settings) error {
	c.section.Set(settings)
	if c.saver == nil {
		return nil
	}
	return c.saver.Update(ctx, sectionName, func() error {
		c.section.Set(settings)
		return nil
	})
}

// SetHistorySize 选择保留最近多少条记录，也就决定历史页面列出多少条。
//
// 历史在此按新上限裁剪，而非等到下次读取：用户要的是最近十条，就不该把更长
// 的列表留在磁盘上直到又复制了别的。被裁掉的条目就此消失——范围由文件持有，
// 而非由页面显示——因此设置在做出选择前会说明这一点。
func (c *Clipboard) SetHistorySize(ctx context.Context, size int) error {
	if !ValidHistorySize(size) {
		size = DefaultHistorySize
	}
	c.mu.Lock()
	if c.settings.HistorySize == size {
		c.mu.Unlock()
		return nil
	}
	c.settings.HistorySize = size
	settings := c.settings
	c.keep = size
	c.history = c.prune(c.history)
	emit := c.emit
	c.mu.Unlock()

	err := c.saveSettings(ctx, settings)
	c.persist()
	if emit != nil {
		emit()
	}
	return err
}

// Start 在设置允许时开始监控。main.go 在应用就绪后调用一次。
func (c *Clipboard) Start(ctx context.Context) {
	if !c.Settings().Monitoring {
		return
	}
	c.begin(ctx)
}

// Stop 结束监控并等待轮询循环，再写一次历史。应用退出时调用。
func (c *Clipboard) Stop() {
	c.stop()
	c.flush()
}

// begin 若尚未运行则启动轮询循环。
func (c *Clipboard) begin(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	watcher := &Watcher{
		Device:   c.device,
		Interval: c.interval,
		Now:      c.now,
		Emit:     c.record,
	}
	done := make(chan struct{})
	c.cancel, c.done, c.watcher = cancel, done, watcher
	c.mu.Unlock()

	// 轮询可能运行数小时，值得一行日志记下它启动：这是应用当时在做什么的唯一记录。
	slog.Info("clipboard monitoring started", "interval", watcher.Interval.String())

	go func() {
		defer close(done)
		if err := watcher.Run(runCtx); err != nil {
			// 循环只会因无法继续的错误退出——设备不存在——且别处不会报告它。
			slog.Warn("the clipboard poll stopped", "error", err)
		}
	}()
}

// stop 取消轮询循环并等待其返回。
func (c *Clipboard) stop() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done, c.watcher = nil, nil, nil
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	slog.Info("clipboard monitoring stopped")
}

// Show 报告界面是否有理由读取剪贴板：栏被绘制且其要绘制的内容正在采集。
func (c *Clipboard) Show() bool {
	return c.Settings().Reads()
}

// Latest 是最新一次读取，以及是否存在。
func (c *Clipboard) Latest() (domain.ClipRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return domain.ClipRecord{}, false
	}
	return *c.latest, true
}

// Open 请求读取一次剪贴板，不论监控开关如何，并立即返回。打开历史窗口时正是如此：
// 用户要求看剪贴板，这是开关从未管辖的读取。
//
// 读取在 goroutine 中进行而非在调用方：调用方正在绘制一帧。Windows 上读取剪贴板
// 可能等待占用剪贴板的其它程序，这段等待每多一毫秒，窗口就多空转一毫秒。
// 读取带来的帧由 emit 钩子在确有内容可显示时请求。
func (c *Clipboard) Open(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go c.Read(ctx)
}

// Read 读取一次剪贴板并等待它，适合不在绘制线程的调用方与测试。它就是 Open 在后台做的事。
func (c *Clipboard) Read(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.poll(ctx)
}

// PolledRecently 报告上个间隔内剪贴板是否读过。刚打开的窗口可先问一句，
// 避免为已有内容再读一次。
func (c *Clipboard) PolledRecently() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.readAt.IsZero() && c.now().Sub(c.readAt) < c.interval
}

// poll 读取设备一次，抑制本应用自己写回的内容，记录余下的部分。
func (c *Clipboard) poll(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	reading, err := c.device.Read(ctx)
	if err != nil {
		return
	}
	c.noteRead()
	if reading.CapturedAt.IsZero() {
		reading.CapturedAt = c.now()
	}
	if !reading.Valid() {
		return
	}
	if c.suppressed(reading) {
		return
	}
	c.record(reading)
}

// noteRead 记下上次读取剪贴板的时间，使刚打开的窗口不会立刻再读一次。
func (c *Clipboard) noteRead() {
	c.mu.Lock()
	c.readAt = c.now()
	c.mu.Unlock()
}

// Suppress 使下一次读取不被记为新复制。即将在自身 goroutine 上写入时会调用它：
// 写入与读到它的读取无序，因此窗口在写入前而非写入后开启。
func (c *Clipboard) Suppress() {
	c.mu.Lock()
	c.writeAt = c.now()
	c.mu.Unlock()
}

// copyIgnoreWindow 是合成复制的值被排除在历史之外的时长。它覆盖复制、
// 从中取出文本的读取以及把剪贴板放回的写入，并为一趟已在途的轮询留出余量。
const copyIgnoreWindow = 2 * time.Second

// IgnoreTheNextCopy 使剪贴板在接下来一刻报告的内容不被记入历史，不论由谁写入。
//
// 划词工具在请求某应用复制前调用它：那次复制并非用户要记录的内容，而是
// 向应用提出的一个问题，工具随后会原样把剪贴板放回。与 Suppress 不同，它不
// 比对读取值是否为本应用所写，因为这次复制并非本应用所写。
func (c *Clipboard) IgnoreTheNextCopy() {
	c.mu.Lock()
	c.ignoreUntil = c.now().Add(copyIgnoreWindow)
	c.mu.Unlock()
}

// record 把一次读取变为当前内容，并在其为新内容时转为一条历史记录。
// 它是 watcher 的回调，运行在 watcher 的 goroutine 上。
func (c *Clipboard) record(reading domain.ClipContent) {
	record := Record(reading)
	if record.Content == "" && record.Display == "" {
		// 无可读内容的富文本：既无可显示也无可匹配，不成条目。
		return
	}
	if len(reading.Image) > 0 && record.ImageRef != "" && !c.keepImage(reading.Image, record.ImageRef) {
		// 图片未能保存。条目仍然保留——用户确实复制了东西——点击它会提示图片已丢失，
		// 而不是粘贴「[图片]」这一行。
		record.ImageRef = ""
	}
	c.mu.Lock()
	c.latest = &record
	changed := c.append(record)
	emit := c.emit
	c.mu.Unlock()
	if changed {
		c.persist()
	}
	if emit != nil {
		emit()
	}
}

// keepImage 把复制图片的字节存到点击其条目时可以取回的地方。
//
// 这是字节唯一能拿到的时刻：等到有人点击时，剪贴板已持有别的内容。它运行在
// watcher 的 goroutine 上，因此是每份复制单独写一个文件，而非并入历史写入——
// 历史在后台写入，而它必须在引用它的条目出现之前就绪。
func (c *Clipboard) keepImage(png []byte, ref string) bool {
	return c.store.SaveImage(context.Background(), ref, png) == nil
}

// append 把一次读取加入历史，除非它已在其中。调用方持有锁。它报告历史是否改变，
// 由此决定是否需要重新写入。
//
// 相同内容再次复制会把其条目移回顶部，而非新增一条：历史记录的是复制过什么，
// 而非复制了几次。对已在剪贴板上的内容的读取——轮询再次询问，或窗口被打开——
// 不算改变，因此不会重写文件。
func (c *Clipboard) append(record domain.ClipRecord) bool {
	if record.Hash == "" || record.Content == "" {
		return false
	}
	for index := range c.history {
		if c.history[index].ContentHash != record.Hash {
			continue
		}
		// 只有列表会显示出来的移动才算改变：同一秒内，条目已在应在的位置。
		if record.CapturedAt.Sub(c.history[index].CopiedAt) < time.Second {
			return false
		}
		c.history[index].CopiedAt = record.CapturedAt
		c.history = c.prune(c.history)
		return true
	}
	id, err := domain.NewID()
	if err != nil {
		// 无人可识别的条目比没有条目更糟：剪贴板只是便利，不能因此拖垮应用。
		return false
	}
	c.history = c.prune(append(c.history, domain.ClipboardEntry{
		ID:          id,
		Content:     record.Content,
		ContentHash: record.Hash,
		Kind:        record.Kind,
		CopiedAt:    record.CapturedAt,
		CreatedAt:   c.now(),
		Display:     record.Display,
		Files:       append([]string(nil), record.Files...),
		ImageBytes:  record.ImageBytes,
		ImageRef:    record.ImageRef,
		Note:        record.Note,
	}))
	return true
}

// prune 按最新在前排序历史，丢弃超过上限的部分，保留置顶条目。调用方持有锁。
func (c *Clipboard) prune(entries []domain.ClipboardEntry) []domain.ClipboardEntry {
	kept := make([]domain.ClipboardEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Content) == "" || entry.ContentHash == "" {
			continue // 既无法显示也无法匹配的条目
		}
		kept = append(kept, entry)
	}
	sort.SliceStable(kept, func(i, j int) bool { return newerEntry(kept[i], kept[j]) })
	if len(kept) <= c.keep {
		return kept
	}
	// 超过上限的条目中，未置顶者移出，置顶者保留其位置，这正是置顶的意义。
	// 列表已是最新在前，因此留下的就是排在前面的。
	room := make([]domain.ClipboardEntry, 0, len(kept))
	unpinned := 0
	for _, entry := range kept {
		if entry.Pinned {
			room = append(room, entry)
			continue
		}
		if unpinned < c.keep {
			room = append(room, entry)
			unpinned++
		}
	}
	// 上限约束的只是未置顶条目：置顶条目正是历史可以超过上限的原因。
	if unpinned > c.keep {
		room = c.evictOldest(room, unpinned-c.keep)
	}
	c.forgetImages(entries, room)
	return room
}

// forgetImages 删除已不在历史中的条目的图片：条目没了，图片就再也放不回来，
// 而只增不减的图片存储就是一个堆满不会再被看到的截图的目录。
//
// 它在自己的 goroutine 上运行：调用方持有锁且正在绘制一帧或轮询剪贴板，
// 删除文件不值得占用其中之一。
func (c *Clipboard) forgetImages(before, after []domain.ClipboardEntry) {
	kept := make(map[string]bool, len(after))
	for _, entry := range after {
		if entry.ImageRef != "" {
			kept[entry.ImageRef] = true
		}
	}
	var dropped []string
	for _, entry := range before {
		if entry.ImageRef != "" && !kept[entry.ImageRef] {
			dropped = append(dropped, entry.ImageRef)
		}
	}
	if len(dropped) == 0 {
		return
	}
	go func() {
		for _, ref := range dropped {
			if err := c.store.DeleteImage(context.Background(), ref); err != nil {
				// 无论如何条目都已从历史移除：留下的只是一个无人再看的文件，
				// 因此记为警告而非失败。
				slog.Warn("a picture of a dropped entry could not be deleted", "error", err)
			}
		}
	}()
}

// evictOldest 从最旧处起删除 n 个未置顶条目，适用于最新在前的列表。调用方持有锁。
func (c *Clipboard) evictOldest(entries []domain.ClipboardEntry, n int) []domain.ClipboardEntry {
	for n > 0 && len(entries) > 0 {
		index := -1
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].Pinned {
				index = i
				break
			}
		}
		if index < 0 {
			break
		}
		entries = append(entries[:index], entries[index+1:]...)
		n--
	}
	return entries
}

// History 返回已记录的历史，最新在前，至多 limit 条。limit 为零或更小则返回存储保留的全部。
func (c *Clipboard) History(limit int) []domain.ClipboardEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.historyLocked(limit)
}

// historyLocked 供已持有锁的调用方使用的 History。
func (c *Clipboard) historyLocked(limit int) []domain.ClipboardEntry {
	entries := c.history
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return append([]domain.ClipboardEntry(nil), entries...)
}

// HistoryError 是存储的历史读不出的原因（若有）。界面会据此说明，
// 而不是显示空历史——那看起来像「从未复制过任何内容」。
func (c *Clipboard) HistoryError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.historyErr
}

// Copy 把一条已记录的复制原样写回剪贴板：文件列表仍为文件列表，图片（字节仍在时）
// 仍为图片，其余作为文本。所属条目会移至历史顶部。
//
// 写入结果会记住两个轮询间隔：否则循环会读回本应用刚写的内容并记为新复制，
// 这正是文档所指出的循环。
func (c *Clipboard) Copy(ctx context.Context, entry domain.ClipboardEntry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.write(ctx, entry); err != nil {
		return err
	}
	c.mu.Lock()
	c.writeValue = writeFingerprint(entry)
	c.writeKind = entry.Kind
	c.writeAt = c.now()
	c.touch(entry)
	c.mu.Unlock()
	c.persist()
	return nil
}

// CopyAsync 把一条已记录的复制写回剪贴板并立即返回，通过 done 报告写入结果。
//
// 点击历史行时调用它。写入本身必须发生在平台线程，而调用方正是绘制点击所在帧
// 的那个线程：在这里写入意味着该帧——以及直到写入返回的后续所有帧——都在
// 剪贴板拿到复制之前绘制。提前返回不会留下半成品：条目在写入前已被 touch，
// 因此无论平台如何回应，点击在列表中都是可见的。
func (c *Clipboard) CopyAsync(entry domain.ClipboardEntry, done func(error)) {
	c.mu.Lock()
	c.writeValue = writeFingerprint(entry)
	c.writeKind = entry.Kind
	c.writeAt = c.now()
	c.touch(entry)
	c.mu.Unlock()
	c.persist()

	go func() {
		err := c.write(context.Background(), entry)
		if done != nil {
			done(err)
		}
	}()
}

// write 把一条已记录的复制以正确的类型写回剪贴板。
//
// 文件列表作为文件写回：这种复制的价值就在于能粘进接受文件的程序。图片作为
// 图片写回从图片存储加载——过去点击图片会把「[图片]」这一行放到剪贴板，
// 那是哪里都粘不了的文本。其余内容作为文本写回。
func (c *Clipboard) write(ctx context.Context, entry domain.ClipboardEntry) error {
	if entry.Kind == domain.ClipKindImage {
		if entry.ImageRef == "" {
			return fmt.Errorf("%w: this copy is a picture whose bytes were not kept",
				domain.ErrUnsupportedClipboardFormat)
		}
		png, err := c.store.LoadImage(ctx, entry.ImageRef)
		if err != nil {
			return err
		}
		return c.device.WriteImage(ctx, png)
	}
	if len(entry.Files) > 0 {
		return c.device.WriteFiles(ctx, entry.Files)
	}
	value := entry.Content
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: there is nothing to copy", domain.ErrInvalidArgument)
	}
	if len(value) > maxCopiedTextBytes {
		return fmt.Errorf("%w: the text is too long to copy", domain.ErrInvalidArgument)
	}
	return c.device.WriteText(ctx, value)
}

// entryHash 是识别一条已记录复制的依据。已带哈希的条目保留它：它由用户复制的
// 原始内容算出，若用历史显示的那一行重算，会把一次复制变成两条。
func entryHash(entry domain.ClipboardEntry) string {
	if entry.ContentHash != "" {
		return entry.ContentHash
	}
	if len(entry.Files) > 0 {
		return kit.ClipContentHash("files:" + strings.Join(entry.Files, "\n"))
	}
	return kit.ClipContentHash(entry.Content)
}

// writeFingerprint 是抑制窗口用于比对读取值的内容：文本复制取文本，
// 文件列表取代表它的那一行。
func writeFingerprint(entry domain.ClipboardEntry) string {
	if entry.Content != "" {
		return strings.TrimSpace(entry.Content)
	}
	return strings.TrimSpace(entry.Display)
}

// touch 把持有某次复制的条目移回顶部，历史从未见过它时则记录之。调用方持有锁。
func (c *Clipboard) touch(entry domain.ClipboardEntry) {
	hash := entryHash(entry)
	for index := range c.history {
		if c.history[index].ContentHash != hash {
			continue
		}
		c.history[index].CopiedAt = c.now()
		c.history = c.prune(c.history)
		return
	}
	_ = c.append(domain.ClipRecord{
		Kind:       entry.Kind,
		Content:    entry.Content,
		Display:    entry.Display,
		Hash:       hash,
		Files:      entry.Files,
		ImageBytes: entry.ImageBytes,
		CapturedAt: c.now(),
	})
}

// suppressed 报告某次读取是否为本应用自己写回的内容。
func (c *Clipboard) suppressed(reading domain.ClipContent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.suppresses(reading)
}

// suppresses 供持有锁的调用方使用的 suppressed。
func (c *Clipboard) suppresses(reading domain.ClipContent) bool {
	if !c.ignoreUntil.IsZero() && c.now().Before(c.ignoreUntil) {
		// 合成复制开启的窗口：其中读到的任何内容都不是用户的复制。
		return true
	}
	if c.writeValue == "" {
		return false
	}
	if c.now().Sub(c.writeAt) > 2*c.interval {
		c.writeValue = ""
		return false
	}
	// 写出的文件列表以相同路径回来；写出的文本以相同文本回来。
	if reading.Kind != c.writeKind {
		return false
	}
	text := reading.Text
	if text == "" {
		text = reading.Display
	}
	if c.writeKind == domain.ClipKindFile {
		text = strings.Join(reading.Files, "\n")
	}
	return strings.TrimSpace(text) == c.writeValue
}

// newerEntry 将两条条目按最新在前排序，相同时按 id 决出先后。
//
// id 是 UUIDv7，其顺序即铸造顺序：同一秒内的两次复制也有稳定顺序，
// 这正是列表不会在帧与帧之间重新洗牌的原因。
func newerEntry(a, b domain.ClipboardEntry) bool {
	if a.CopiedAt.Equal(b.CopiedAt) {
		return a.ID > b.ID
	}
	return a.CopiedAt.After(b.CopiedAt)
}

// persist 请求写入历史并立即返回。
//
// 就地写入会把一份历史大小的文件（可达数百条）挡在点击与「点击已生效」的帧之间。
// 后台写入者取最新快照，且已在途的写入会让下次调用成为空操作：写入期间的点击
// 会并入其后那次写入，而不是排队第二次。
func (c *Clipboard) persist() {
	c.mu.Lock()
	c.dirty = true
	if c.writing || c.store == nil {
		c.mu.Unlock()
		return
	}
	c.writing = true
	c.mu.Unlock()

	go func() {
		for {
			c.mu.Lock()
			if !c.dirty {
				c.writing = false
				c.mu.Unlock()
				return
			}
			entries := append([]domain.ClipboardEntry(nil), c.history...)
			c.dirty = false
			c.mu.Unlock()
			// 内存中的历史是工作副本，而这个文件是进程结束后唯一留存的东西：
			// 写入失败就意味着本次运行的复制在下次启动时消失，而别处不会提示。
			if err := c.store.SaveEntries(context.Background(), entries); err != nil {
				slog.Warn("the clipboard history could not be written", "error", err)
			}
		}
	}()
}

// settled 报告没有待写的内容且没有在途写入。测试借此等待历史落盘。
func (c *Clipboard) settled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dirty && !c.writing
}

// flush 立即写入历史并等待它，供即将不存在的调用方使用。
func (c *Clipboard) flush() {
	c.mu.Lock()
	entries := append([]domain.ClipboardEntry(nil), c.history...)
	c.dirty = false
	store := c.store
	c.mu.Unlock()
	if store == nil {
		return
	}
	// 这是本次运行的最后一次写入，此处失败最易被用户注意、也最难被告知。
	if err := store.SaveEntries(context.Background(), entries); err != nil {
		slog.Warn("the clipboard history could not be written as the application quit", "error", err)
	}
}

// Record 是服务保留并绘制的那一次读取：剪贴板当前内容，浓缩为一行，
// 附带用于匹配的哈希和类型。
//
// Content 是历史条目写回剪贴板的值：文本复制即文本本身，文件列表或图片则为
// 路径与名称，即平台能写的形式。Display 是工具两面都展示的那一行，
// 使十条复制的列表仍然是一个列表。
func Record(reading domain.ClipContent) domain.ClipRecord {
	switch reading.Kind {
	case domain.ClipKindFile:
		display := kit.ClipFileDisplay(reading.Files)
		content := strings.Join(reading.Files, "\n")
		if content == "" {
			content = display
		}
		return domain.ClipRecord{
			Kind:       reading.Kind,
			Content:    kit.ClipText(content),
			Display:    display,
			Hash:       kit.ClipContentHash("files:" + strings.Join(reading.Files, "\n")),
			Files:      append([]string(nil), reading.Files...),
			ImageBytes: reading.ImageBytes,
			ImageRef:   reading.ImageRef,
			Note:       reading.Observation,
			CapturedAt: reading.CapturedAt,
		}
	case domain.ClipKindImage:
		name := ""
		if len(reading.Files) > 0 {
			name = reading.Files[0]
		}
		display := "[" + kit.ClipImageName(name) + "]"
		// 读取带有字节时哈希就是图片本身，使两张截图成为两条；没有字节时
		// 只有平台提供的名称，于是无名图片都成了同一张——这正是过去的问题。
		hash := kit.ClipContentHash("image:" + kit.ClipFileName(name))
		if reading.ImageRef != "" {
			hash = kit.ClipContentHash("image:" + reading.ImageRef)
		}
		return domain.ClipRecord{
			Kind:       reading.Kind,
			Content:    display,
			Display:    display,
			Hash:       hash,
			Files:      append([]string(nil), reading.Files...),
			ImageBytes: reading.ImageBytes,
			ImageRef:   reading.ImageRef,
			Note:       reading.Observation,
			CapturedAt: reading.CapturedAt,
		}
	default:
		text := reading.Text
		if strings.TrimSpace(text) == "" && reading.HTML != "" {
			// 富文本没有附带纯文本：文本由标记呈现的内容决定。
			text = kit.ExtractHTMLText(reading.HTML)
		}
		kept := kit.ClipText(text)
		display := kit.ClipDisplay(kept)
		if display == "" {
			// 无可读内容的富文本仍是一次复制，界面必须有所显示而非空白。
			display = kit.ClipDisplay(kit.ExtractHTMLText(reading.HTML))
		}
		return domain.ClipRecord{
			Kind:       kit.ClassifyClip(kept),
			Content:    kept,
			Display:    display,
			Hash:       kit.ClipContentHash(kept),
			CapturedAt: reading.CapturedAt,
		}
	}
}

// errorsIsInvalidArgument 报告错误是否为领域层表示「调用方请求了不可能的操作」的方式。
