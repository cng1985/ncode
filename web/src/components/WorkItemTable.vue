<script setup>
import { ArrowRight, Files, Tickets } from '@element-plus/icons-vue'

defineProps({
  items: { type: Array, default: () => [] },
  loading: Boolean,
})
const emit = defineEmits(['open'])

function dueDate(value) {
  if (!value) return '未设置'
  const [year, month, day] = value.split('-')
  return `${year}年${Number(month)}月${Number(day)}日`
}

function statusClass(name) {
  if (name === '进行中') return 'status-progress'
  if (name === '待评审') return 'status-review'
  if (name === '待验收') return 'status-acceptance'
  if (name === '已验收') return 'status-done'
  return 'status-default'
}
</script>

<template>
  <div class="work-table-wrap" v-loading="loading">
    <table v-if="items.length" class="work-table">
      <thead>
        <tr>
          <th>项目</th>
          <th>工作项</th>
          <th>状态</th>
          <th>截止日期</th>
          <th class="table-action"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id" tabindex="0" @click="emit('open', item)" @keydown.enter="emit('open', item)">
          <td class="project-cell">
            <span class="project-cell-content">
            <span class="project-glyph"><el-icon><Files /></el-icon></span>
            <span class="cell-copy">
              <strong>{{ item.projectName }}</strong>
              <small>{{ item.type === 'BUG' ? '缺陷' : item.type === 'STORY' ? '需求' : item.type === 'DEBT' ? '技术债' : '产品与交付' }}</small>
            </span>
            </span>
          </td>
          <td class="task-cell">
            <strong>{{ item.title }}</strong>
            <small>#{{ item.code }} · {{ item.ownerName || '待认领' }}</small>
          </td>
          <td><span class="status-text" :class="statusClass(item.displayStateName)">{{ item.displayStateName }}</span></td>
          <td class="due-cell">{{ dueDate(item.dueDate) }}</td>
          <td class="table-action"><el-icon><ArrowRight /></el-icon></td>
        </tr>
      </tbody>
    </table>
    <div v-else-if="!loading" class="table-empty">
      <el-icon><Tickets /></el-icon>
      <strong>还没有工作项</strong>
      <span>创建第一项工作，让项目开始向前推进。</span>
    </div>
  </div>
</template>
