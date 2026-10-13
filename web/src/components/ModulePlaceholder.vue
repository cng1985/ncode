<script setup>
import { Connection } from '@element-plus/icons-vue'

defineProps({ title: { type: String, required: true } })
</script>

<template>
  <main class="module-placeholder">
    <div class="placeholder-mark"><el-icon><Connection /></el-icon></div>
    <h1>{{ title }}</h1>
    <p>这个模块还没有接入当前试点版本。已有的项目与工作项数据会保留在工作台中。</p>
  </main>
</template>
