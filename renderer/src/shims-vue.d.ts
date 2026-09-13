/**
 * tsgolint 不认识 .vue 文件，这里给出最小声明。
 * 注意：必须是全局（无顶层 import/export）文件，通配声明才生效。
 * SFC 内部类型检查由 vue-tsc 负责（接入见 Task 003）。
 */
declare module "*.vue" {
  import type { DefineComponent } from "vue";

  const component: DefineComponent;
  export default component;
}
