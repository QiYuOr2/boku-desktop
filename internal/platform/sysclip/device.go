// Package sysclip 通过 MyGo 读写系统剪贴板。
//
// 每次 MyGo 剪贴板调用都会触碰平台，而剪贴板由主线程拥有，MyGo 自身的调用
// 会等待该线程。因此本包通过装配层提供的 dispatch 函数来执行这些调用，让在
// goroutine 上运行的轮询不会阻塞事件循环，窗口关闭时轮询也不会卡死。一次
// dispatch 若在超时内没有答复就会被丢弃：片刻后还会有下一次轮询，一次读取
// 抵不过一个卡住的应用。
//
// 拿到结果后的处理是纯函数——输入格式、文本、标记与文件路径，输出
// domain.ClipContent——它单独测试，因为平台那部分在没有会话的测试进程里跑不了。
package sysclip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"boku/internal/domain"
	"boku/internal/kit"
)

// DefaultTimeout 限制一次主线程往返的时长：超时未完成的剪贴板读取本应用
// 不需要，片刻后还有下一次轮询，关闭窗口也不该等剪贴板。
//
// 这个值给得宽，是因为其中一种读取是位图：把 Windows 位图截图转成 PNG 要几十
// 毫秒，一次因太慢被丢弃的读取，就是一张没有字节的图片。
const DefaultTimeout = 2 * time.Second

// pictureFresh 是本设备读过的图片可以继续交出、而不必重新读取的时长。
//
// 剪贴板一次只存一个值，图片留在上面不会变化，所以读过后片刻仍在提供的图片
// 就是当时读到的图片。窗口只约束这个假设持续多久：它覆盖几个轮询间隔，让留在
// 剪贴板上的截图只读一次而非每次轮询都读；又足够短，用户在此期间新复制的图片
// 不会被长时间当作旧图交出。
const pictureFresh = 5 * time.Second

// Dispatcher 在平台要求的线程上运行 fn 并等待其完成；没有该线程时（应用正在
// 退出）返回 false。它正是让非主线程轮询安全的原因。
type Dispatcher func(fn func()) bool

// Device 是平台剪贴板。
//
// 同一时刻只有一个读取方（即轮询循环）使用它。
type Device struct {
	dispatch    Dispatcher
	timeout     time.Duration
	now         func() time.Time
	changeCount func() uint32

	// pictureStamp 是 picture 被读取的时刻，pictureCount 是当时的剪贴板写计数，
	// picture 是平台交出的那张图片。
	//
	// 三者共同避免对仍在剪贴板上的图片重复读取。查看图片是本设备唯一昂贵的工作：
	// 平台以设备无关位图提供位图，MyGo 逐像素解码再整图编码成 PNG，一屏像素要
	// 一百多毫秒，期间剪贴板一直锁住，别处应用的复制会直接失败。每 700ms 轮询
	// 一次就意味着截图留在剪贴板上的整段时间都在付这个代价、并锁住剪贴板。
	//
	// 计数器让缓存保持诚实：仅靠时间窗口会把片刻前已被替换的图片继续交出，因为
	// 复制不会拨动时钟。Windows 上计数器来自 GetClipboardSequenceNumber，macOS
	// 上来自 NSPasteboard.changeCount；没有计数器的平台报告 0，退化为时间窗口。
	//
	// 保留而非即时测量，是因为点击历史中的图片会把它放回去，而那是唯一能读到它的
	// 时刻，也因为下一次轮询可以再次交出它而不必重读。
	picture       []byte
	pictureRef    string
	pictureStamp  time.Time
	pictureChange uint32
}

// New 返回通过 dispatch 读取的设备。dispatch 为 nil 时在调用方 goroutine 上执行
// 剪贴板调用，测试所需的正是这种。
func New(dispatch Dispatcher) *Device {
	return &Device{
		dispatch:    dispatch,
		timeout:     DefaultTimeout,
		now:         func() time.Time { return time.Now().UTC() },
		changeCount: clipboardChangeCount,
	}
}

// ChangeCount 是剪贴板自身的写计数器：平台每次写入都会递增，无论谁写。它让
// 一次读取得知剪贴板确实变了，而不只是读取恰好更晚，划词工具也用它区分
// 「没有选中」与「同样的文本又被复制了一次」。
//
// 与所有 MyGo 剪贴板调用一样，平台粘贴板在主线程上读取；调用方已在主线程时
// 内联运行。没有计数器的平台返回 0，调用方必须把 0 当作「无答案」而不是稳定值。
func (d *Device) ChangeCount() uint32 {
	if d == nil || d.changeCount == nil {
		return 0
	}
	var count uint32
	mygo.RunOnMain(func() { count = d.changeCount() })
	return count
}

// currentChangeCount 在调用方当前所在线程读取计数器，供设备自身使用：
// snapshot 运行在主线程，测试则没有事件循环。
func (d *Device) currentChangeCount() uint32 {
	if d == nil || d.changeCount == nil {
		return 0
	}
	return d.changeCount()
}

// Read 返回剪贴板当前内容；当它没有本应用展示的内容时返回
// domain.ErrUnsupportedClipboardFormat。
func (d *Device) Read(ctx context.Context) (domain.ClipContent, error) {
	var (
		content domain.ClipContent
		err     error
	)
	if !d.call(ctx, func() {
		content, err = d.snapshot()
	}) {
		return domain.ClipContent{}, domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return domain.ClipContent{}, err
	}
	content.CapturedAt = d.now()
	return content, nil
}

// WriteText 把文本放到剪贴板。
func (d *Device) WriteText(ctx context.Context, text string) error {
	// 平台调用不返回结果；被其它进程占用的剪贴板由平台自行重试。
	d.call(ctx, func() { mygo.Clipboard.WriteText(text) })
	return nil
}

// WriteFiles 把文件列表放到剪贴板，使点击一条已记录的文件副本时粘贴的是文件本身
// 而不是其路径文本。
func (d *Device) WriteFiles(ctx context.Context, paths []string) error {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			cleaned = append(cleaned, path)
		}
	}
	if len(cleaned) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	var err error
	ran := d.call(ctx, func() {
		// 平台不接受的路径（相对路径，或已不存在的路径）由平台给出答复。
		err = mygo.Clipboard.WriteFiles(cleaned...)
	})
	if !ran {
		return domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return fmt.Errorf("%w: write the file list: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return nil
}

// WriteImage 把图片放到剪贴板：点击历史中的图片，以及划词工具在读取选中内容后
// 归还它借走的剪贴板内容。
//
// 字节是平台交出的 PNG，原样放回。
func (d *Device) WriteImage(ctx context.Context, png []byte) error {
	if len(png) == 0 {
		return domain.ErrUnsupportedClipboardFormat
	}
	var err error
	ran := d.call(ctx, func() { err = mygo.Clipboard.WriteImage(png) })
	if !ran {
		return domain.ErrUnsupportedClipboardFormat
	}
	if err != nil {
		return fmt.Errorf("%w: write the image: %v", domain.ErrUnsupportedClipboardFormat, err)
	}
	return nil
}

// call 通过 dispatcher 运行 fn，并报告它是否执行。超时仍未答复的 dispatcher 被
// 抛下：本函数返回，它等待的 goroutine 在主线程最终处理到它时结束。
func (d *Device) call(ctx context.Context, fn func()) bool {
	if d.dispatch == nil {
		fn()
		return true
	}
	done := make(chan bool, 1)
	go func() { done <- d.dispatch(fn) }()

	timeout := d.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var canceled <-chan struct{}
	if ctx != nil {
		canceled = ctx.Done()
	}
	select {
	case ran := <-done:
		return ran
	case <-timer.C:
		return false
	case <-canceled:
		return false
	}
}

// snapshot 是对平台剪贴板的一次读取，运行在拥有剪贴板的线程上。
//
// 它只读一次剪贴板公布的格式，然后基于该结果处理。
//
// 图片是唯一昂贵的一类：片刻前本设备读过的同一张图片会直接交出而非重读；
// 见 pictureFresh。其余内容照旧读取。
func (d *Device) snapshot() (domain.ClipContent, error) {
	formats := mygo.Clipboard.Formats()
	if len(formats) == 0 {
		// 剪贴板为空，或调用被应用退出丢弃：两种情况都没有可展示的内容。
		d.forgetPicture()
		return domain.ClipContent{}, domain.ErrUnsupportedClipboardFormat
	}
	facts := platformFacts{
		formats:  formats,
		hasImage: hasImage(formats),
		imageErr: domain.ErrUnsupportedClipboardFormat,
	}
	if facts.hasImage {
		switch {
		case d.pictureStillThere():
			// 同一张图片仍在剪贴板上：它就是之前读到的，本次读取直接采用，
			// 重读只会花掉一屏的解码却一无所获。
			facts.image = d.picture
			facts.imageRef = d.pictureRef
		default:
			// 计数器在读取前取：若解码位图期间剪贴板被写入，记住的图片归属旧
			// 计数器，下一次轮询会重新读取而不是继续交出。
			count := d.currentChangeCount()
			png, err := mygo.Clipboard.ReadFormat(transfer.PNG)
			facts.image, facts.imageErr = png, err
			if err == nil && len(png) > 0 {
				d.rememberPictureAt(png, d.now(), count)
			} else {
				// 什么都没读到，就没有可再交出的图片：下一次轮询必须重新尝试。
				d.forgetPicture()
			}
		}
	} else {
		d.forgetPicture()
	}
	// 文本与文件列表逐类读取，某类缺失不算错误：上面有图片的剪贴板只是上面
	// 没有别的东西。
	//
	// 文本由平台自身的辅助函数读取：Windows 与 Linux 沿用旧的 ReadText，
	// macOS 则协商格式，因为有些复制只以 public.utf16-plain-text 到达。HTML
	// 单独读取，使畸形的 HTML 不会连文本一起拖垮。
	facts.text = readClipboardText()
	facts.html = mygo.Clipboard.ReadHTML()
	if paths, err := mygo.Clipboard.ReadFiles(); err == nil {
		facts.files = paths
	}
	return Convert(facts), nil
}

// hasFormat 报告剪贴板是否提供某格式。
func hasFormat(formats []transfer.Format, want transfer.Format) bool {
	for _, format := range formats {
		if format == want {
			return true
		}
	}
	return false
}

// pictureStillThere 报告本设备读过的图片是否仍是剪贴板当前提供的图片。
//
// 之所以安全，是因为剪贴板一次只存一个值：图片不变则剪贴板不变，而本应用读取
// 剪贴板的频率高于人去复制。时间窗口约束这个假设，万一图片超过窗口还在，就重新
// 读取而不是永远交出它。
func (d *Device) pictureStillThere() bool {
	if len(d.picture) == 0 || d.pictureRef == "" {
		return false
	}
	if d.pictureStamp.IsZero() {
		return false
	}
	if d.pictureChange != 0 && d.pictureChange != d.currentChangeCount() {
		// 此图读取后剪贴板又被写入：现在上面无论是什么都不是记住的那张，
		return false
	}
	return d.now().Sub(d.pictureStamp) < pictureFresh
}

// rememberPicture 保存刚读到的图片，好让下一次轮询直接交出而不是重新读取编码。
func (d *Device) rememberPicture(png []byte, at time.Time) {
	d.rememberPictureAt(png, at, d.currentChangeCount())
}

// rememberPictureAt 是带读取起始时剪贴板计数器的 rememberPicture，正是它防止读取
// 期间的写入被当作此前读到的图片缓存下来。
func (d *Device) rememberPictureAt(png []byte, at time.Time, count uint32) {
	d.picture = png
	d.pictureRef = kit.ClipImageRef(png)
	d.pictureStamp = at
	d.pictureChange = count
}

// forgetPicture 丢弃本设备持有的图片。
//
// 一次没找到图片、或没能读出图片的读取就留下这个结果：没有可再交出的内容，而
// 保留先前的图片会让下一次读取声称持有一张并不存在的图片。
func (d *Device) forgetPicture() {
	d.picture, d.pictureRef, d.pictureStamp, d.pictureChange = nil, "", time.Time{}, 0
}

// platformFacts 是一次读取平台剪贴板的结果，以纯值表示：即读取中无需会话就能
// 测试的那部分。
type platformFacts struct {
	formats  []transfer.Format
	text     string
	html     string
	files    []string
	hasImage bool
	// image 是剪贴板上的位图（平台交出的形式），仅在读到时有值；imageErr 是
	// 格式表明有位图却没拿到时缺失的原因。保留字节是因为图片必须能放回去——
	// 点击历史中的它，或划词工具归还剪贴板——而这是唯一能读到它的时刻。
	image    []byte
	imageErr error
	// imageRef 是图片的名字，由读取它的那一方命名。携带图片的读取也携带其名字：
	// 此处再命名会把字节再哈希一次，而在字节被丢弃后命名则会命名错误。
	imageRef string
}

// Convert 把一次平台读取转换成应用记录的内容。
//
// 顺序决定用户本意：文件列表就是文件列表，尽管平台同时把第一个路径以文本放进
// 剪贴板；而裸位图是平台对它持有的内容没有名字时剩下的东西。
func Convert(facts platformFacts) domain.ClipContent {
	switch {
	case len(facts.files) > 0:
		content := domain.ClipContent{
			Kind:       domain.ClipKindFile,
			Files:      facts.files,
			HasImage:   facts.hasImage,
			ImageBytes: len(facts.image),
			Image:      facts.image,
			ImageRef:   facts.imageRef,
		}
		if facts.hasImage && len(facts.image) == 0 && facts.imageErr != nil {
			content = content.WithObservation(
				"图片没能读出：" + facts.imageErr.Error() + "；已记下文件名")
		}
		return content
	case facts.hasImage:
		content := domain.ClipContent{
			Kind:       domain.ClipKindImage,
			HasImage:   true,
			ImageBytes: len(facts.image),
			Image:      facts.image,
			ImageRef:   facts.imageRef,
		}
		if len(facts.image) == 0 {
			reason := "剪贴板没有给出图片数据"
			if facts.imageErr != nil {
				reason = facts.imageErr.Error()
			}
			content = content.WithObservation("图片没能读出：" + reason)
		}
		return content
	case strings.TrimSpace(facts.text) != "" || strings.TrimSpace(facts.html) != "":
		return domain.ClipContent{Kind: domain.ClipKindText, Text: facts.text, HTML: facts.html}
	default:
		return domain.ClipContent{}
	}
}

// hasImage 报告剪贴板是否提供图片。
func hasImage(formats []transfer.Format) bool {
	for _, format := range formats {
		if format == transfer.PNG {
			return true
		}
	}
	return false
}

// 设备即用例所求的接口实现。
var _ domain.ClipboardDevice = (*Device)(nil)
