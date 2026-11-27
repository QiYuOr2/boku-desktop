// Package popoverappearance 负责强制深色外观所需的 macOS 桥接。
//
// NSPopover 的材质跟随它所依附的视图（对 Boku 是菜单栏按钮），而非应用强制的外观。
// 在浅色桌面上强制深色时，页面是深色而弹窗仍为浅色。MyGo 未暴露
// NSPopover.appearance，因此本包通过 MyGo 暴露的句柄拿到弹窗原生窗口，并为其设置
// vibrant dark 外观。
package popoverappearance

// Controller 给已创建的原生弹窗设置深色外观。
type Controller struct{}

// New 返回平台对应的 controller。
func New() *Controller { return &Controller{} }

// Available 报告当前平台能否设置弹窗外观。
func Available() bool { return available() }
