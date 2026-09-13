import { contextBridge } from "electron";

import type { BokuApi } from "@boku/desktop-shared";

/**
 * renderer 与 main 之间唯一的桥梁。
 *
 * 只暴露业务 API，不暴露 ipcRenderer，也不提供任意 SQL 执行入口。
 * 见 docs/architecture.md 与 docs/api.md。
 */
const api: BokuApi = {};

contextBridge.exposeInMainWorld("api", api);
