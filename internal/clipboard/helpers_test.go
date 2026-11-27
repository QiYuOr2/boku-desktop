package clipboard

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"boku/internal/domain"
	"boku/internal/kit"
)

// 剪贴板功能测试所用的替身，由本包多个测试文件共享：设备、历史存储、
// 设置存储，以及一个由测试驱动的时钟。

// fakeDevice 是内容由测试决定的 domain.ClipboardDevice。
type fakeDevice struct {
	// now 为设备交回的内容打时间戳，使测试自己的时钟而非墙钟决定复制发生的时间。
	now func() time.Time

	// readDelay 与 writeDelay 模拟被其它程序占用的平台剪贴板：
	// 调用要等这么久才返回。它们在设备使用前设定，从不在使用中设定。
	readDelay  time.Duration
	writeDelay time.Duration

	mu      sync.Mutex
	reading domain.ClipContent
	err     error
	reads   int
	writes  []string
	files   [][]string
	images  [][]byte
	writeEr error
}

// failWrite 使下一次写入失败，模拟平台拒收该格式。
func (d *fakeDevice) failWrite(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeEr = err
}

func (d *fakeDevice) set(reading domain.ClipContent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.now != nil {
		reading.CapturedAt = d.now()
	}
	d.reading = reading
}

func (d *fakeDevice) fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

func (d *fakeDevice) Read(context.Context) (domain.ClipContent, error) {
	if delay := d.readDelay; delay > 0 {
		time.Sleep(delay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	if d.err != nil {
		return domain.ClipContent{}, d.err
	}
	return d.reading, nil
}

func (d *fakeDevice) WriteText(_ context.Context, text string) error {
	if delay := d.writeDelay; delay > 0 {
		time.Sleep(delay)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.writes = append(d.writes, text)
	d.reading = domain.ClipContent{
		Kind: domain.ClipKindText,
		Text: text,
	}
	if d.now != nil {
		d.reading.CapturedAt = d.now()
	}
	return nil
}

func (d *fakeDevice) WriteFiles(_ context.Context, paths []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.files = append(d.files, append([]string(nil), paths...))
	d.reading = domain.ClipContent{Kind: domain.ClipKindFile, Files: append([]string(nil), paths...)}
	if d.now != nil {
		d.reading.CapturedAt = d.now()
	}
	return nil
}

// WriteImage 把图片放到剪贴板，即点击历史中一张图片时的行为。
func (d *fakeDevice) WriteImage(_ context.Context, png []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.writeEr != nil {
		return d.writeEr
	}
	d.images = append(d.images, append([]byte(nil), png...))
	return nil
}

func (d *fakeDevice) readCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

func (d *fakeDevice) written() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.writes...)
}

func (d *fakeDevice) writtenFiles() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]string, 0, len(d.files))
	for _, paths := range d.files {
		out = append(out, append([]string(nil), paths...))
	}
	return out
}

func (d *fakeDevice) writtenImages() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][]byte, 0, len(d.images))
	for _, png := range d.images {
		out = append(out, append([]byte(nil), png...))
	}
	return out
}

// fakeStore 是内存中的历史。
type fakeStore struct {
	mu      sync.Mutex
	entries []domain.ClipboardEntry
	saves   int
	loadErr error
	saveErr error
	// images 是历史旁的图片存储：复制图片的字节，以其条目保留的引用为键。
	images map[string][]byte
}

func (s *fakeStore) LoadEntries(context.Context) ([]domain.ClipboardEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return append([]domain.ClipboardEntry(nil), s.entries...), nil
}

// SaveImage 是图片存储：同一引用保存两次是同一张图片，真实存储也如此。
func (s *fakeStore) SaveImage(_ context.Context, ref string, png []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.images == nil {
		s.images = map[string][]byte{}
	}
	if _, ok := s.images[ref]; ok {
		return nil
	}
	s.images[ref] = append([]byte(nil), png...)
	return nil
}

func (s *fakeStore) LoadImage(_ context.Context, ref string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	png, ok := s.images[ref]
	if !ok {
		return nil, fmt.Errorf("%w: no picture was kept", domain.ErrUnsupportedClipboardFormat)
	}
	return append([]byte(nil), png...), nil
}

func (s *fakeStore) DeleteImage(_ context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.images, ref)
	return nil
}

// keptImages 是图片存储当前持有的内容，供测试查看。
func (s *fakeStore) keptImages() map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte, len(s.images))
	for ref, png := range s.images {
		out[ref] = append([]byte(nil), png...)
	}
	return out
}

func (s *fakeStore) SaveEntries(_ context.Context, entries []domain.ClipboardEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.entries = append([]domain.ClipboardEntry(nil), entries...)
	return nil
}

func (s *fakeStore) stored() []domain.ClipboardEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ClipboardEntry(nil), s.entries...)
}

// fakeTools 是内存中的设置存储：本功能的 section，用 section 本身代替文件。
//
// section 是真实的那个，因此通过它改变的开关与正式代码改变的方式一致；被伪造的
// 只是底下的文件。loadErr 与 saveErr 分开，因为测试需要指明两者中哪个失败：
// 读不出的文件会阻止应用启动，写不出的文件不得连累用户刚拨的开关。
type fakeTools struct {
	mu      sync.Mutex
	section *Section
	saves   int
	loadErr error
	saveErr error
}

func newFakeTools() *fakeTools {
	return &fakeTools{section: NewSection()}
}

func (f *fakeTools) Load(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadErr
}

// Update 是存储的读-改-写，在一个临界区里完成：功能用它修改设置，
// 而不会覆盖别人的改动。
func (f *fakeTools) Update(_ context.Context, _ string, change func() error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	return change()
}

// testClock 是由测试手动拨动的时钟。
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newTestClipboard 在替身之上装配工具。
func newTestClipboard(t *testing.T, opts ClipboardOptions) (*Clipboard, *fakeDevice, *fakeStore, *fakeTools) {
	t.Helper()
	device := &fakeDevice{}
	store := &fakeStore{}
	tools := newFakeTools()
	opts.Device = device
	opts.Store = store
	if opts.Section == nil {
		opts.Section = tools.section
	}
	if opts.Settings == nil {
		opts.Settings = tools
	}
	if opts.Clock == nil {
		opts.Clock = newTestClock()
	}
	// 设备用它交回内容时使用与服务相同的时钟，使测试只比较一条时间线而非两条。
	if clock, ok := opts.Clock.(*testClock); ok {
		device.now = clock.Now
	}
	clipboard, err := NewClipboard(opts)
	if err != nil {
		t.Fatalf("NewClipboard failed: %v", err)
	}
	t.Cleanup(clipboard.Stop)
	return clipboard, device, store, tools
}

// read 按历史窗口的方式轮询剪贴板，晚测试时钟一拍：两次复制从不是同一瞬间，
// 因此历史保留的顺序就是它们发生的顺序。
//
// 它用 Read 而非 Open：Open 是界面的非阻塞请求，而测试希望下一行时读取已经发生。
func read(clipboard *Clipboard, clock *testClock, device *fakeDevice, text string) {
	clock.advance(time.Second)
	device.set(textReading(text))
	clipboard.Read(context.Background())
}

func textReading(text string) domain.ClipContent {
	return domain.ClipContent{Kind: domain.ClipKindText, Text: text, Display: text}
}

// textEntry 是某段文本的已记录复制，即历史持有的形式。
func textEntry(content string) domain.ClipboardEntry {
	return domain.ClipboardEntry{
		ID:          "entry-" + content,
		Content:     content,
		ContentHash: kit.ClipContentHash(content),
		Kind:        kit.ClassifyClip(content),
		Display:     kit.ClipDisplay(content),
		CopiedAt:    time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
}

// waitFor 轮询条件直至成立，若始终不成立则令测试失败：等待 goroutine 是测试
// 唯一可以 sleep 的地方。
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
