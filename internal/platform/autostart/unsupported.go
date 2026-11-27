package autostart

// Unsupported 表示该桌面没有 Boku 能注册的登录启动。
//
// 它的存在是为了让应用其余部分只需一条代码路径：每个平台都回答同样四个问题，
// 而这个平台对四个问题都返回 ErrUnsupported。把错误当作「开关已关」的调用方只有
// 忽略该错误时才会出错；设置页面在绘制任何东西前会先检查这个错误。
type Unsupported struct{}

// Read 报告该桌面没有登录启动。
func (Unsupported) Read(Launcher) (bool, error) { return false, ErrUnsupported }

// Location 报告没有可展示的位置。
func (Unsupported) Location(Launcher) (string, error) { return "", ErrUnsupported }

// Start 拒绝：该桌面没有可启动的东西。
func (Unsupported) Start(Launcher) error { return ErrUnsupported }

// Stop 同样拒绝：这里从未启动过任何东西。
func (Unsupported) Stop(Launcher) error { return ErrUnsupported }
