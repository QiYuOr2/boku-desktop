/**
 * 跨进程共享的契约层。
 *
 * 只放不依赖 Electron、不依赖 SQLite 的内容：
 * Record 类型、ID / 时间规则、Markdown / Wiki Link 解析、IPC 契约。
 *
 * 见 docs/architecture.md 与 docs/api.md。
 * Task 001 只建立包边界，具体契约从 Task 003 起加入。
 */

/** preload 暴露给 renderer 的 API 契约，业务通道后续按 docs/api.md 增加。 */
export type BokuApi = Record<string, never>;
