import { describe, expect, it } from "vite-plus/test";

import * as shared from "./index.ts";

/**
 * Task 001 的管线冒烟测试：证明 vitest 能跑起来。
 * Task 003 起替换为真实的 Record 契约测试。
 */
describe("shared 包", () => {
  it("可以被加载", () => {
    expect(shared).toBeTypeOf("object");
  });
});
