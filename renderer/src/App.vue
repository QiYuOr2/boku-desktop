<script setup lang="ts">
// Task 001 只做外壳：Timeline 是默认首页（见 docs/product.md），
// 数据与交互从 Task 005 起实现。
</script>

<template>
  <main class="app">
    <h1 class="title">Boku</h1>
    <p class="empty">还没有记录。Timeline 会在后续 Task 中实现。</p>
  </main>
</template>

<style scoped>
.app {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 2rem;
  font-family:
    system-ui,
    -apple-system,
    "Segoe UI",
    sans-serif;
}

.title {
  margin: 0;
  font-size: 1.5rem;
}

.empty {
  margin: 0;
  color: #666666;
}
</style>
