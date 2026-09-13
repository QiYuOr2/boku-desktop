/**
 * Desktop 开发编排。
 *
 * 1. 启动 renderer 的 vp dev（固定端口）
 * 2. 以 watch 模式构建 main 与 preload
 * 3. 产物就绪后启动 Electron，加载 dev server
 * 4. main / preload 重新构建后重启 Electron
 *
 * 见 docs/spec/desktop-toolchain.md。
 */
import { spawn, type ChildProcess } from "node:child_process";
import { existsSync, watch } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const desktopDir = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
const rendererDir = path.join(desktopDir, "renderer");
const mainDir = path.join(desktopDir, "main");
const preloadDir = path.join(desktopDir, "preload");
const mainDist = path.join(mainDir, "dist");
const preloadDist = path.join(preloadDir, "dist");
const mainEntry = path.join(mainDist, "index.cjs");
const preloadEntry = path.join(preloadDist, "index.cjs");

const devServerPort = 5173;
const devServerUrl = `http://localhost:${devServerPort}`;
const buildTimeoutMs = 60_000;
const pollIntervalMs = 200;

const require = createRequire(import.meta.url);
const electronBinary = require("electron") as string;

const children = new Set<ChildProcess>();
let shuttingDown = false;

function shutdown(code: number): void {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const child of children) child.kill();
  process.exit(code);
}

function start(label: string, args: string[], cwd: string): ChildProcess {
  const child = spawn("vp", args, { cwd, stdio: "inherit" });
  children.add(child);
  child.on("exit", (code, signal) => {
    if (shuttingDown) return;
    console.error(`[dev] ${label} 退出 (code=${code}, signal=${signal})`);
    shutdown(1);
  });
  return child;
}

function launchElectron(): ChildProcess {
  const child = spawn(electronBinary, [desktopDir], {
    cwd: desktopDir,
    stdio: "inherit",
    env: { ...process.env, BOKU_DEV_SERVER_URL: devServerUrl },
  });
  children.add(child);
  child.on("exit", () => {
    if (shuttingDown) return;
    console.log("[dev] Electron 已退出，关闭开发编排");
    shutdown(0);
  });
  return child;
}

function delay(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
}

async function waitForHttp(url: string): Promise<void> {
  const deadline = Date.now() + buildTimeoutMs;
  while (Date.now() < deadline) {
    try {
      await fetch(url);
      return;
    } catch {
      await delay();
    }
  }
  throw new Error(`等待 renderer dev server 超时：${url}`);
}

async function waitForFile(file: string): Promise<void> {
  const deadline = Date.now() + buildTimeoutMs;
  while (Date.now() < deadline) {
    if (existsSync(file)) return;
    await delay();
  }
  throw new Error(`等待构建产物超时：${file}`);
}

/** 监听产物目录，构建完成后去抖触发回调。 */
function onRebuild(dir: string, onChange: () => void): void {
  let timer: NodeJS.Timeout | undefined;
  watch(dir, (_event, filename) => {
    if (!filename?.endsWith(".cjs")) return;
    clearTimeout(timer);
    timer = setTimeout(onChange, 150);
  });
}

async function main(): Promise<void> {
  start("renderer", ["dev"], rendererDir);
  start("main", ["build", "--watch"], mainDir);
  start("preload", ["build", "--watch"], preloadDir);

  await waitForHttp(devServerUrl);
  await waitForFile(mainEntry);
  await waitForFile(preloadEntry);

  let electron = launchElectron();
  const restart = (): void => {
    electron.kill();
    electron = launchElectron();
  };
  onRebuild(mainDist, restart);
  onRebuild(preloadDist, restart);

  console.log(`[dev] Electron 已启动，dev server: ${devServerUrl}`);
}

process.on("SIGINT", () => shutdown(0));
process.on("SIGTERM", () => shutdown(0));

main().catch((error: unknown) => {
  console.error(error);
  shutdown(1);
});
