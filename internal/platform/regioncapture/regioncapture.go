// Package regioncapture 读取桌面矩形容器内的像素。
//
// 本包不呈现任何 UI，也不接触剪贴板：原生选择浮层由应用层负责，本包只负责
// 操作系统像素读取与 PNG 编码。
package regioncapture

type Device struct{}

func New() *Device { return &Device{} }
