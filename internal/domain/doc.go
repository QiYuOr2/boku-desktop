// Package domain 是应用的契约：实体、外围各层实现的接口，以及它们返回的错误。
//
// 它不依赖任何东西——没有界面、没有 SQLite、没有 HTTP、没有 MyGo——
// 这正是其它各层可替换、测试可离线的原因。这里不出现任何厂商名：
// provider 的形状由 adapter 转换成这些实体。
package domain
