import path from "node:path";

import { app, BrowserWindow, shell } from "electron";

/** dev 由 scripts/dev.ts 注入；生产为空，改为加载本地文件。 */
const devServerUrl = process.env.BOKU_DEV_SERVER_URL;

function createWindow(): BrowserWindow {
  const appRoot = app.getAppPath();
  const mainWindow = new BrowserWindow({
    width: 1200,
    height: 800,
    minWidth: 720,
    minHeight: 480,
    show: false,
    webPreferences: {
      preload: path.join(appRoot, "preload/dist/index.cjs"),
      // 安全基线，见 docs/architecture.md
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });

  mainWindow.once("ready-to-show", () => mainWindow.show());

  // 外部链接交给系统浏览器，不在应用内打开新窗口
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url);
    return { action: "deny" };
  });

  if (devServerUrl) {
    void mainWindow.loadURL(devServerUrl);
    mainWindow.webContents.openDevTools({ mode: "detach" });
  } else {
    void mainWindow.loadFile(path.join(appRoot, "renderer/dist/index.html"));
  }

  return mainWindow;
}

async function bootstrap(): Promise<void> {
  await app.whenReady();
  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});

void bootstrap();
