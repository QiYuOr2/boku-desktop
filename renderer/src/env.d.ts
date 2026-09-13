import type { BokuApi } from "@boku/desktop-shared";

/** preload 通过 contextBridge 暴露的唯一命名空间。 */
declare global {
  interface Window {
    api: BokuApi;
  }
}

/**
 * tsgolint 不认识 .vue 文件，这里给出最小声明。
 * SFC 内部类型检查由 vue-tsc 负责（接入见 Task 003）。
 */
export {};
