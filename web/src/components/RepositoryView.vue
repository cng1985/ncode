<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Document, Folder, Plus } from '@element-plus/icons-vue'
import { api } from '../api'

const props = defineProps({ projects: { type: Array, default: () => [] } })
const emit = defineEmits(['create-project'])
const repositories = ref([])
const loading = ref(false)
const error = ref('')
const createDialog = ref(false)
const creating = ref(false)
const activeRepository = ref(null)
const currentPath = ref('')
const entries = ref([])
const selectedFile = ref(null)
const browserBusy = ref(false)
const form = reactive({ projectId: null, name: '', slug: '', visibility: 'private' })

const breadcrumbs = computed(() => currentPath.value ? currentPath.value.split('/').map((name, index, parts) => ({ name, path: parts.slice(0, index + 1).join('/') })) : [])
const activeProjectName = computed(() => props.projects.find((project) => project.id === activeRepository.value?.projectId)?.name || activeRepository.value?.projectName || '')

function slugFor(value) {
  const slug = value.normalize('NFKD').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
  return slug || `repo-${Date.now()}`
}

function openCreate() {
  Object.assign(form, { projectId: props.projects[0]?.id || null, name: '', slug: '', visibility: 'private' })
  createDialog.value = true
}

function updateSlug() {
  form.slug = slugFor(form.name)
}

async function loadRepositories(selectId = null) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.repositories()
    repositories.value = result.data
    const next = repositories.value.find((repo) => repo.id === selectId) || repositories.value.find((repo) => repo.id === activeRepository.value?.id) || repositories.value[0]
    if (next) await openRepository(next)
    else {
      activeRepository.value = null
      entries.value = []
      selectedFile.value = null
    }
  } catch (cause) {
    error.value = cause.message
  } finally {
    loading.value = false
  }
}

async function createRepository() {
  creating.value = true
  try {
    const result = await api.createRepository({ ...form })
    createDialog.value = false
    ElMessage.success('仓库已初始化，首个提交已创建')
    await loadRepositories(result.data.id)
  } catch (cause) {
    ElMessage.error(cause.message)
  } finally {
    creating.value = false
  }
}

async function openRepository(repository) {
  activeRepository.value = repository
  currentPath.value = ''
  selectedFile.value = null
  await loadTree('')
}

async function loadTree(path) {
  if (!activeRepository.value) return
  browserBusy.value = true
  try {
    const result = await api.repositoryTree(activeRepository.value.id, path, activeRepository.value.defaultBranch)
    currentPath.value = result.data.path
    entries.value = result.data.entries
  } catch (cause) {
    ElMessage.error(cause.message)
  } finally {
    browserBusy.value = false
  }
}

async function openEntry(entry) {
  selectedFile.value = null
  if (entry.type === 'tree') {
    await loadTree(entry.path)
    return
  }
  browserBusy.value = true
  try {
    selectedFile.value = (await api.repositoryFile(activeRepository.value.id, entry.path, activeRepository.value.defaultBranch)).data
  } catch (cause) {
    ElMessage.error(cause.message)
  } finally {
    browserBusy.value = false
  }
}

function goToParent() {
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  loadTree(parts.join('/'))
}

onMounted(loadRepositories)
</script>

<template>
  <main class="repository-layout">
    <aside class="repository-sidebar">
      <div class="repository-sidebar-heading"><div><h1>仓库</h1><p>项目代码与文件浏览</p></div><el-button circle aria-label="新建仓库" @click="projects.length ? openCreate() : emit('create-project')"><el-icon><Plus /></el-icon></el-button></div>
      <el-button v-if="projects.length" class="repository-create-button" type="primary" @click="openCreate"><el-icon><Plus /></el-icon>新建仓库</el-button>
      <div v-if="loading && !repositories.length" class="repository-list-loading" v-loading="loading"></div>
      <div v-else-if="repositories.length" class="repository-list">
        <button v-for="repository in repositories" :key="repository.id" class="repository-list-item" :class="{ active: activeRepository?.id === repository.id }" @click="openRepository(repository)">
          <span class="repository-mark">{{ repository.name.slice(0, 1).toUpperCase() }}</span>
          <span><strong>{{ repository.name }}</strong><small>{{ repository.projectName }} · {{ repository.defaultBranch }}</small></span>
        </button>
      </div>
      <div v-else-if="!loading" class="repository-empty-list">
        <strong>还没有仓库</strong>
        <span v-if="projects.length">为一个项目初始化仓库，系统会创建 main 分支和 README。</span>
        <span v-else>先创建项目，再创建仓库。</span>
        <el-button v-if="projects.length" text type="primary" @click="openCreate">创建第一个仓库</el-button>
        <el-button v-else text type="primary" @click="emit('create-project')">先创建项目</el-button>
      </div>
      <div v-if="error" class="repository-error" role="alert"><span>{{ error }}</span><el-button text @click="loadRepositories">重试</el-button></div>
    </aside>

    <section class="repository-content">
      <template v-if="activeRepository">
        <header class="repository-header">
          <div class="repository-title"><span class="repository-mark large">{{ activeRepository.name.slice(0, 1).toUpperCase() }}</span><div><h2>{{ activeRepository.name }}</h2><p>{{ activeProjectName }} · 工作区内可见</p></div></div>
          <el-tag effect="plain">{{ activeRepository.state === 'active' ? '可用' : '初始化中' }}</el-tag>
        </header>
        <div class="repository-toolbar">
          <span class="repository-branch"><span class="branch-glyph">⑂</span>{{ activeRepository.defaultBranch }}</span>
          <span class="repository-readonly">当前提供受权限保护的只读浏览</span>
        </div>
        <div class="repository-browser" v-loading="browserBusy">
          <div class="repository-tree-pane">
            <div class="repository-pathbar">
              <button v-if="currentPath" class="path-back" aria-label="返回上级目录" @click="goToParent"><el-icon><ArrowLeft /></el-icon></button>
              <button class="path-root" @click="loadTree('')">{{ activeRepository.slug }}</button>
              <template v-for="crumb in breadcrumbs" :key="crumb.path"><span>/</span><button @click="loadTree(crumb.path)">{{ crumb.name }}</button></template>
            </div>
            <div v-if="entries.length" class="repository-file-list">
              <button v-for="entry in entries" :key="entry.path" class="repository-file-row" :class="{ selected: selectedFile?.path === entry.path }" @click="openEntry(entry)">
                <el-icon><Folder v-if="entry.type === 'tree'" /><Document v-else /></el-icon><span>{{ entry.name }}</span><small>{{ entry.type === 'tree' ? '目录' : '文件' }}</small>
              </button>
            </div>
            <div v-else-if="!browserBusy" class="repository-empty-tree">这个目录没有文件。</div>
          </div>
          <div class="repository-file-pane">
            <template v-if="selectedFile"><div class="file-preview-heading"><el-icon><Document /></el-icon><strong>{{ selectedFile.path }}</strong><small>{{ selectedFile.size.toLocaleString() }} bytes</small></div><pre class="file-preview">{{ selectedFile.content }}</pre></template>
            <div v-else class="repository-file-hint"><el-icon><Document /></el-icon><strong>选择文件以查看内容</strong><span>文件浏览固定到 {{ activeRepository.defaultBranch }} 分支。</span></div>
          </div>
        </div>
      </template>
      <div v-else-if="!loading" class="repository-welcome"><span class="repository-welcome-mark"><el-icon><Folder /></el-icon></span><h2>代码仓库</h2><p>从项目创建一个仓库，开始整理可追溯的代码交付。</p><el-button v-if="projects.length" type="primary" @click="openCreate">新建仓库</el-button><el-button v-else type="primary" @click="emit('create-project')">先创建项目</el-button></div>
    </section>

    <el-dialog v-model="createDialog" title="新建仓库" width="min(520px, calc(100vw - 32px))" destroy-on-close>
      <el-form :model="form" label-position="top">
        <el-form-item label="所属项目" required><el-select v-model="form.projectId" class="full-width"><el-option v-for="project in projects" :key="project.id" :label="project.name" :value="project.id" /></el-select></el-form-item>
        <el-form-item label="仓库名称" required><el-input v-model="form.name" maxlength="120" placeholder="例如：ncode-api" @input="updateSlug" /></el-form-item>
        <el-form-item label="仓库标识" required><el-input v-model="form.slug" maxlength="80" placeholder="ncode-api" /></el-form-item>
        <el-form-item label="可见范围"><el-tag effect="plain">工作区内可见</el-tag></el-form-item>
        <p class="repository-dialog-note">仓库将创建 main 分支和首个 README 提交，仅对当前工作区成员开放浏览。</p>
      </el-form>
      <template #footer><el-button @click="createDialog = false">取消</el-button><el-button type="primary" :loading="creating" :disabled="!form.projectId || !form.name.trim() || !form.slug.trim()" @click="createRepository">创建仓库</el-button></template>
    </el-dialog>
  </main>
</template>
