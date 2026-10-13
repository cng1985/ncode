<script setup>
import { computed } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import WorkItemTable from './WorkItemTable.vue'

const props = defineProps({
  projects: { type: Array, default: () => [] },
  items: { type: Array, default: () => [] },
  loading: Boolean,
  search: { type: String, default: '' },
})
const emit = defineEmits(['create-project', 'create-work-item', 'open-item', 'open-project', 'update:search', 'retry'])

const filteredItems = computed(() => {
  const keyword = props.search.trim().toLocaleLowerCase()
  if (!keyword) return props.items
  return props.items.filter((item) => `${item.title} ${item.projectName} ${item.code} ${item.ownerName}`.toLocaleLowerCase().includes(keyword))
})

const activeProjects = computed(() => props.projects.filter((project) => project.state === 1).slice(0, 5))
</script>

<template>
  <main class="workbench-layout">
    <section class="workbench-main">
      <div class="welcome-row">
        <div>
          <h1>继续推进你的项目</h1>
          <p>今天先处理最重要的交付。</p>
        </div>
        <el-button class="primary-action" type="primary" size="large" @click="emit('create-project')">
          <el-icon><Plus /></el-icon>
          新建项目
        </el-button>
      </div>

      <div class="section-heading task-heading">
        <h2>我的工作项</h2>
        <el-button class="text-action" @click="emit('create-work-item')">
          <el-icon><Plus /></el-icon>新建工作项
        </el-button>
      </div>
      <WorkItemTable :items="filteredItems" :loading="loading" @open="emit('open-item', $event)" />
      <div v-if="!loading && filteredItems.length === 0 && items.length > 0" class="filtered-empty">
        没有找到匹配的工作项。<button @click="emit('update:search', '')">清除搜索</button>
      </div>
    </section>

    <aside class="workbench-rail">
      <section class="rail-section">
        <div class="section-heading rail-heading">
          <h2>我的项目</h2>
          <button class="inline-link" @click="emit('open-project')">全部 {{ projects.length }}</button>
        </div>
        <div v-if="activeProjects.length" class="project-progress-list">
          <button v-for="project in activeProjects" :key="project.id" class="progress-project" @click="emit('open-project', project.id)">
            <span class="go-mark">Go</span>
            <span class="progress-copy">
              <strong>{{ project.name }}</strong>
              <small>{{ project.goal || '继续完善项目交付' }}</small>
              <span class="progress-line"><i :style="{ width: `${project.progress}%` }"></i></span>
            </span>
            <span class="progress-value">{{ project.progress }}%</span>
          </button>
        </div>
        <div v-else-if="!loading" class="rail-empty">创建项目后，进展会显示在这里。</div>
      </section>

      <section class="rail-section due-section">
        <div class="section-heading rail-heading"><h2>近期截止</h2></div>
        <button v-for="item in items.filter((row) => row.dueDate).slice(0, 3)" :key="item.id" class="deadline-row" @click="emit('open-item', item)">
          <span class="deadline-dot" :class="item.displayStateName === '已验收' ? 'dot-done' : ''"></span>
          <span><strong>{{ item.title }}</strong><small>{{ item.projectName }} · {{ item.dueDate }}</small></span>
        </button>
        <div v-if="!items.some((item) => item.dueDate) && !loading" class="rail-empty">设置工作项截止日期，便于安排下一步。</div>
      </section>
    </aside>
  </main>
</template>
