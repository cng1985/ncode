<script setup>
import { computed } from 'vue'
import { Plus } from '@element-plus/icons-vue'

const props = defineProps({
  projects: { type: Array, default: () => [] },
  selectedProjectId: { type: Number, default: null },
  items: { type: Array, default: () => [] },
  loading: Boolean,
  search: { type: String, default: '' },
})
const emit = defineEmits(['select', 'create-project', 'create-work-item', 'open-item'])
const currentProject = computed(() => props.projects.find((project) => project.id === props.selectedProjectId) || null)
const filteredProjects = computed(() => {
  const keyword = props.search.trim().toLocaleLowerCase()
  if (!keyword) return props.projects
  return props.projects.filter((project) => `${project.name} ${project.slug} ${project.goal}`.toLocaleLowerCase().includes(keyword) || props.items.some((item) => item.projectId === project.id && `${item.title} ${item.code} ${item.ownerName}`.toLocaleLowerCase().includes(keyword)))
})
const filteredItems = computed(() => {
  const selected = props.items.filter((item) => item.projectId === props.selectedProjectId)
  const keyword = props.search.trim().toLocaleLowerCase()
  if (!keyword) return selected
  return selected.filter((item) => `${item.title} ${item.code} ${item.ownerName}`.toLocaleLowerCase().includes(keyword))
})
</script>

<template>
  <main class="project-layout">
    <section class="project-list-pane">
      <div class="page-title-row">
        <div><h1>项目</h1><p>把目标、工作项和交付进度放在一起。</p></div>
        <el-button type="primary" class="primary-action" @click="emit('create-project')"><el-icon><Plus /></el-icon>新建项目</el-button>
      </div>
      <div v-if="filteredProjects.length" class="project-list">
        <button v-for="project in filteredProjects" :key="project.id" class="project-list-row" :class="{ selected: selectedProjectId === project.id }" @click="emit('select', project.id)">
          <span class="go-mark">Go</span>
          <span class="project-list-copy"><strong>{{ project.name }}</strong><small>{{ project.goal || '暂无项目目标' }}</small><small>{{ project.itemCount }} 个工作项 · {{ project.stateName }}</small></span>
          <span class="project-list-progress"><b>{{ project.progress }}%</b><i><em :style="{ width: `${project.progress}%` }"></em></i></span>
        </button>
      </div>
      <div v-else-if="!loading" class="project-empty"><strong>还没有项目</strong><span>从一个清晰的目标开始，逐步完成可验证的交付。</span><el-button type="primary" @click="emit('create-project')">创建第一个项目</el-button></div>
      <div v-else class="project-loading" v-loading="loading"></div>
    </section>

    <section class="project-detail-pane">
      <template v-if="currentProject">
        <div class="detail-title-row">
          <div><h2>{{ currentProject.name }}</h2><p>{{ currentProject.goal }}</p></div>
          <el-button class="text-action" @click="emit('create-work-item', selectedProjectId)"><el-icon><Plus /></el-icon>新建工作项</el-button>
        </div>
        <div class="section-heading project-task-heading"><h3>工作项</h3><span>{{ filteredItems.length }} 项</span></div>
        <div class="project-items">
          <button v-for="item in filteredItems" :key="item.id" class="project-item-row" @click="emit('open-item', item)">
            <span class="project-item-state" :class="`state-${item.state}`"></span>
            <span><strong>{{ item.title }}</strong><small>{{ item.code }} · {{ item.ownerName || '待认领' }}</small></span>
            <span class="status-text">{{ item.displayStateName }}</span>
          </button>
          <div v-if="!filteredItems.length && !loading" class="rail-empty">没有找到匹配的工作项。</div>
        </div>
      </template>
      <div v-else class="select-project-hint">选择一个项目，查看它的目标和工作项。</div>
    </section>
  </main>
</template>
