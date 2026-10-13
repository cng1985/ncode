<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Bell, Briefcase, CircleCheck, Close, Connection, Document, Folder, House, Monitor, Plus, Reading, Search, User } from '@element-plus/icons-vue'
import { api } from './api'
import WorkbenchView from './components/WorkbenchView.vue'
import ProjectView from './components/ProjectView.vue'
import ServiceView from './components/ServiceView.vue'
import ModulePlaceholder from './components/ModulePlaceholder.vue'

const navItems = [
  { label: '工作台', icon: House },
  { label: '项目', icon: Folder },
  { label: '服务', icon: Monitor },
  { label: '仓库', icon: Connection },
  { label: '学习', icon: Reading },
  { label: '作品', icon: Document },
  { label: '机会', icon: Briefcase },
]
const mobileNav = [
  { label: '工作台', icon: House },
  { label: '项目', icon: Folder },
  { label: '服务', icon: Monitor },
  { label: '学习', icon: Reading },
  { label: '我的', icon: User },
]
const projects = ref([])
const workItems = ref([])
const activities = ref([])
const loading = ref(true)
const loadError = ref('')
const activeView = ref('工作台')
const search = ref('')
const selectedProjectId = ref(null)
const projectDialog = ref(false)
const workItemDialog = ref(false)
const detailDrawer = ref(false)
const selectedItem = ref(null)
const projectBusy = ref(false)
const workItemBusy = ref(false)
const saving = ref(false)
const slugTouched = ref(false)

const projectForm = reactive({ name: '', slug: '', goal: '', type: 'product', visibility: 'private' })
const workItemForm = reactive({ projectId: null, title: '', description: '', type: 'TASK', priority: 'MEDIUM', ownerName: '我', dueDate: '', criteria: [{ text: '', checked: false, position: 0 }] })
const draft = reactive({ id: null, expectedVersion: 1, title: '', description: '', type: 'TASK', priority: 'MEDIUM', ownerName: '', dueDate: '', criteria: [] })

const selectedProject = computed(() => projects.value.find((project) => project.id === selectedProjectId.value) || null)
const selectedProjectItems = computed(() => workItems.value.filter((item) => item.projectId === selectedProjectId.value))
const dateHeading = computed(() => new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' }).format(new Date()))
const acceptanceComplete = computed(() => draft.criteria.length > 0 && draft.criteria.every((criterion) => criterion.checked))
const detailAction = computed(() => {
  if (!selectedItem.value) return null
  if (selectedItem.value.state === 0) return { action: 'READY', label: '设为就绪' }
  if (selectedItem.value.state === 1) return { action: 'START', label: '开始处理' }
  if (selectedItem.value.state === 2) return { action: 'REQUEST_REVIEW', label: '提交评审' }
  if (selectedItem.value.state === 3 && selectedItem.value.acceptanceState !== 'PENDING') return { action: 'MARK_ACCEPTANCE_PENDING', label: '标记待验收' }
  if (selectedItem.value.state === 4) return { action: 'REOPEN', label: '重新打开' }
  return null
})

function makeSlug(value) {
  const slug = value.normalize('NFKD').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
  return slug || `project-${Date.now()}`
}

function resetProjectForm() {
  Object.assign(projectForm, { name: '', slug: '', goal: '', type: 'product', visibility: 'private' })
  slugTouched.value = false
}

function updateProjectSlug() {
  if (!slugTouched.value) projectForm.slug = makeSlug(projectForm.name)
}

function openProjectDialog() {
  resetProjectForm()
  projectDialog.value = true
}

function openWorkItemDialog(projectId = null) {
  const targetProject = projectId || selectedProjectId.value || projects.value[0]?.id || null
  Object.assign(workItemForm, { projectId: targetProject, title: '', description: '', type: 'TASK', priority: 'MEDIUM', ownerName: '我', dueDate: '', criteria: [{ text: '', checked: false, position: 0 }] })
  workItemDialog.value = true
}

function addCriterion(form) {
  form.criteria.push({ text: '', checked: false, position: form.criteria.length })
}

function removeCriterion(form, index) {
  form.criteria.splice(index, 1)
  form.criteria.forEach((item, position) => { item.position = position })
}

async function refresh() {
  loading.value = true
  loadError.value = ''
  try {
    const page = await api.projects()
    projects.value = page.data
    if (selectedProjectId.value && !projects.value.some((project) => project.id === selectedProjectId.value)) selectedProjectId.value = null
    if (!selectedProjectId.value && projects.value.length) selectedProjectId.value = projects.value[0].id
    const workPages = await Promise.all(projects.value.map((project) => api.workItems(project.id)))
    workItems.value = workPages.flatMap((result) => result.data).sort((a, b) => (a.dueDate || '9999').localeCompare(b.dueDate || '9999'))
  } catch (error) {
    loadError.value = error.message
  } finally {
    loading.value = false
  }
}

async function createProject() {
  projectBusy.value = true
  try {
    const result = await api.createProject({ ...projectForm })
    projectDialog.value = false
    activeView.value = '项目'
    selectedProjectId.value = result.data.id
    await refresh()
    ElMessage.success('项目已创建')
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    projectBusy.value = false
  }
}

async function createWorkItem() {
  const criteria = workItemForm.criteria.map((item, position) => ({ ...item, text: item.text.trim(), position })).filter((item) => item.text)
  workItemBusy.value = true
  try {
    const result = await api.createWorkItem({ ...workItemForm, criteria })
    workItemDialog.value = false
    await refresh()
    openItem(result.data)
    ElMessage.success('工作项已创建')
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    workItemBusy.value = false
  }
}

function openItem(item) {
  selectedItem.value = item
  Object.assign(draft, {
    id: item.id,
    expectedVersion: item.version,
    title: item.title,
    description: item.description,
    type: item.type,
    priority: item.priority,
    ownerName: item.ownerName,
    dueDate: item.dueDate,
    criteria: item.criteria.map((criterion) => ({ id: criterion.id, text: criterion.text, checked: criterion.checked, position: criterion.position })),
  })
  detailDrawer.value = true
  api.activity(item.id).then((result) => { activities.value = result.data }).catch(() => { activities.value = [] })
}

async function saveItem() {
  saving.value = true
  try {
    const result = await api.updateWorkItem({
      id: draft.id,
      expectedVersion: draft.expectedVersion,
      title: draft.title,
      description: draft.description,
      type: draft.type,
      priority: draft.priority,
      ownerName: draft.ownerName,
      dueDate: draft.dueDate,
      criteria: draft.criteria.map((item, position) => ({ text: item.text.trim(), checked: item.checked, position })),
    })
    await refresh()
    selectedItem.value = result.data
    Object.assign(draft, {
      expectedVersion: result.data.version,
      criteria: result.data.criteria.map((criterion) => ({ id: criterion.id, text: criterion.text, checked: criterion.checked, position: criterion.position })),
    })
    activities.value = (await api.activity(result.data.id)).data
    ElMessage.success('工作项已保存')
  } catch (error) {
    ElMessage.error(error.message)
    if (error.status === 409) await refresh()
  } finally {
    saving.value = false
  }
}

async function transition(action) {
  if (!selectedItem.value) return
  let reason = ''
  if (action === 'REOPEN') {
    try {
      const result = await ElMessageBox.prompt('记录重新打开的原因，便于后续追踪。', '重新打开工作项', { inputPlaceholder: '请输入原因', inputValidator: (value) => Boolean(value?.trim()) || '请填写原因' })
      reason = result.value
    } catch { return }
  }
  saving.value = true
  try {
    const result = await api.transitionWorkItem({ id: selectedItem.value.id, expectedVersion: selectedItem.value.version, action, reason })
    await refresh()
    openItem(result.data)
    ElMessage.success('状态已更新')
  } catch (error) {
    ElMessage.error(error.message)
    if (error.status === 409) await refresh()
  } finally {
    saving.value = false
  }
}

async function acceptItem() {
  if (!selectedItem.value || !acceptanceComplete.value) return
  saving.value = true
  try {
    const result = await api.acceptWorkItem({
      id: selectedItem.value.id,
      expectedVersion: selectedItem.value.version,
      checklistVersion: selectedItem.value.checklistVersion,
      acceptanceStatement: '本人依据当前验收清单完成验收',
    })
    await refresh()
    openItem(result.data)
    ElMessage.success('工作项已验收')
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    saving.value = false
  }
}

function goToProject(id = null) {
  activeView.value = '项目'
  if (id) selectedProjectId.value = id
}

function openCreateWorkItem(projectId = null) {
  if (!projects.value.length) {
    ElMessage.info('请先创建项目')
    openProjectDialog()
    return
  }
  openWorkItemDialog(projectId)
}

function setView(label) {
  activeView.value = label
}

function closeDrawer() {
  detailDrawer.value = false
  selectedItem.value = null
}

function formattedDate(value) {
  if (!value) return '未设置'
  const [year, month, day] = value.split('-')
  return `${year}年${Number(month)}月${Number(day)}日`
}

function activityAction(action) {
  return ({ CREATED: '创建了工作项', UPDATED: '更新了工作项', READY: '将工作项设为就绪', START: '开始处理工作项', REQUEST_REVIEW: '提交了评审', MARK_ACCEPTANCE_PENDING: '标记为待验收', ACCEPTED: '完成了工作项验收', REOPEN: '重新打开工作项', CANCEL: '取消了工作项' })[action] || action
}

onMounted(refresh)
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand-lockup"><span class="brand-symbol">N</span><span>NCode</span></div>
      <nav class="side-nav" aria-label="主导航">
        <button v-for="item in navItems" :key="item.label" class="nav-item" :class="{ active: activeView === item.label }" @click="setView(item.label)">
          <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
        </button>
      </nav>
      <div class="sidebar-bottom"><span class="workspace-dot"></span><span>个人空间</span></div>
    </aside>

    <div class="main-column">
      <header class="topbar">
        <div class="mobile-brand"><span class="brand-symbol">N</span><span>NCode</span></div>
        <div class="topbar-title"><h2>{{ activeView }}</h2><span v-if="activeView === '工作台'">{{ dateHeading }}</span></div>
        <div class="topbar-tools">
          <el-input v-model="search" class="global-search" :placeholder="activeView === '服务' ? '搜索服务名称或命令' : '搜索项目、工作项'" clearable :aria-label="activeView === '服务' ? '搜索服务' : '搜索项目和工作项'">
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <el-popover placement="bottom-end" :width="230" trigger="click">
            <template #reference><button class="icon-button" aria-label="通知"><el-icon><Bell /></el-icon></button></template>
            <div class="notification-empty"><strong>暂无新通知</strong><span>项目动态和评审消息会显示在这里。</span></div>
          </el-popover>
          <button class="avatar" aria-label="个人空间">N</button>
        </div>
      </header>

      <div v-if="loadError" class="error-banner" role="alert">
        <span>{{ loadError }} 请确认 API 与 PostgreSQL 已启动。</span>
        <el-button text @click="refresh">重试</el-button>
      </div>

      <WorkbenchView
        v-if="activeView === '工作台'"
        :projects="projects" :items="workItems" :loading="loading" :search="search"
        @create-project="openProjectDialog" @create-work-item="openCreateWorkItem" @open-item="openItem"
        @open-project="goToProject" @retry="refresh" @update:search="search = $event"
      />
      <ProjectView
        v-else-if="activeView === '项目'"
        :projects="projects" :selected-project-id="selectedProjectId" :items="workItems" :loading="loading" :search="search"
        @select="selectedProjectId = $event" @create-project="openProjectDialog"
        @create-work-item="openCreateWorkItem" @open-item="openItem"
      />
      <ServiceView v-else-if="activeView === '服务'" :search="search" />
      <ModulePlaceholder v-else :title="activeView" />
    </div>

    <nav class="mobile-nav" aria-label="移动端主导航">
      <button v-for="item in mobileNav" :key="item.label" :class="{ active: activeView === item.label || (item.label === '我的' && !['工作台', '项目', '服务', '学习'].includes(activeView)) }" @click="setView(item.label)">
        <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
      </button>
    </nav>
  </div>

  <el-dialog v-model="projectDialog" title="新建项目" width="min(540px, calc(100vw - 32px))" destroy-on-close @closed="resetProjectForm">
    <el-form :model="projectForm" label-position="top" @submit.prevent="createProject">
      <el-form-item label="项目名称" required><el-input v-model="projectForm.name" maxlength="120" placeholder="例如：NCode 官网" @input="updateProjectSlug" /></el-form-item>
      <el-form-item label="项目标识" required><el-input v-model="projectForm.slug" maxlength="80" placeholder="ncode-website" @input="slugTouched = true" /></el-form-item>
      <el-form-item label="项目目标"><el-input v-model="projectForm.goal" type="textarea" :rows="3" maxlength="500" placeholder="描述这个项目要完成的目标" /></el-form-item>
      <div class="form-two-columns">
        <el-form-item label="项目类型"><el-select v-model="projectForm.type"><el-option label="产品项目" value="product" /><el-option label="学习项目" value="learning" /><el-option label="开源项目" value="open_source" /><el-option label="团队项目" value="team" /></el-select></el-form-item>
        <el-form-item label="可见范围"><el-select v-model="projectForm.visibility"><el-option label="私有" value="private" /><el-option label="组织内" value="internal" /><el-option label="公开" value="public" /></el-select></el-form-item>
      </div>
      <div class="dialog-actions"><el-button @click="projectDialog = false">取消</el-button><el-button type="primary" :loading="projectBusy" :disabled="!projectForm.name.trim() || !projectForm.slug.trim()" @click="createProject">创建项目</el-button></div>
    </el-form>
  </el-dialog>

  <el-dialog v-model="workItemDialog" title="新建工作项" width="min(560px, calc(100vw - 32px))" destroy-on-close>
    <el-form :model="workItemForm" label-position="top">
      <el-form-item label="所属项目" required><el-select v-model="workItemForm.projectId" class="full-width"><el-option v-for="project in projects" :key="project.id" :label="project.name" :value="project.id" /></el-select></el-form-item>
      <el-form-item label="工作项标题" required><el-input v-model="workItemForm.title" maxlength="180" placeholder="写下清楚、可交付的一项工作" /></el-form-item>
      <el-form-item label="说明"><el-input v-model="workItemForm.description" type="textarea" :rows="3" placeholder="补充背景、范围或约束" /></el-form-item>
      <div class="form-two-columns">
        <el-form-item label="类型"><el-select v-model="workItemForm.type"><el-option label="任务" value="TASK" /><el-option label="需求" value="STORY" /><el-option label="缺陷" value="BUG" /><el-option label="技术债" value="DEBT" /></el-select></el-form-item>
        <el-form-item label="优先级"><el-select v-model="workItemForm.priority"><el-option label="低" value="LOW" /><el-option label="普通" value="MEDIUM" /><el-option label="高" value="HIGH" /><el-option label="紧急" value="URGENT" /></el-select></el-form-item>
      </div>
      <div class="form-two-columns">
        <el-form-item label="负责人"><el-input v-model="workItemForm.ownerName" placeholder="姓名或待认领" /></el-form-item>
        <el-form-item label="截止日期"><el-date-picker v-model="workItemForm.dueDate" type="date" value-format="YYYY-MM-DD" format="YYYY年MM月DD日" placeholder="选择日期" /></el-form-item>
      </div>
      <div class="criteria-editor">
        <div class="criteria-editor-title"><span>验收清单</span><button type="button" class="inline-link" @click="addCriterion(workItemForm)">添加一项</button></div>
        <div v-for="(criterion, index) in workItemForm.criteria" :key="index" class="criterion-edit-row">
          <el-input v-model="criterion.text" placeholder="怎样才算完成？" />
          <button type="button" class="remove-criterion" aria-label="移除验收项" @click="removeCriterion(workItemForm, index)"><el-icon><Close /></el-icon></button>
        </div>
      </div>
      <div class="dialog-actions"><el-button @click="workItemDialog = false">取消</el-button><el-button type="primary" :loading="workItemBusy" :disabled="!workItemForm.projectId || !workItemForm.title.trim()" @click="createWorkItem">创建工作项</el-button></div>
    </el-form>
  </el-dialog>

  <el-drawer v-model="detailDrawer" :title="selectedItem?.code || '工作项详情'" direction="rtl" size="min(560px, 100vw)" @closed="closeDrawer">
    <template v-if="selectedItem">
      <div class="drawer-content">
        <div class="drawer-status-row"><span class="status-text">{{ selectedItem.displayStateName }}</span><span>{{ selectedItem.projectName }}</span><span>版本 {{ selectedItem.version }}</span></div>
        <el-input v-model="draft.title" class="drawer-title-input" maxlength="180" aria-label="工作项标题" />
        <el-input v-model="draft.description" class="drawer-description" type="textarea" :rows="4" placeholder="添加工作项说明" />
        <div class="form-two-columns drawer-fields">
          <el-form-item label="负责人"><el-input v-model="draft.ownerName" /></el-form-item>
          <el-form-item label="截止日期"><el-date-picker v-model="draft.dueDate" type="date" value-format="YYYY-MM-DD" format="YYYY年MM月DD日" /></el-form-item>
          <el-form-item label="类型"><el-select v-model="draft.type"><el-option label="任务" value="TASK" /><el-option label="需求" value="STORY" /><el-option label="缺陷" value="BUG" /><el-option label="技术债" value="DEBT" /></el-select></el-form-item>
          <el-form-item label="优先级"><el-select v-model="draft.priority"><el-option label="低" value="LOW" /><el-option label="普通" value="MEDIUM" /><el-option label="高" value="HIGH" /><el-option label="紧急" value="URGENT" /></el-select></el-form-item>
        </div>
        <section class="drawer-section">
          <div class="section-heading"><div><h3>验收清单</h3><small>清单版本 {{ selectedItem.checklistVersion }}</small></div><button class="inline-link" @click="addCriterion(draft)">添加一项</button></div>
          <div v-if="draft.criteria.length" class="drawer-criteria">
            <div v-for="(criterion, index) in draft.criteria" :key="criterion.id || index" class="drawer-criterion">
              <el-checkbox v-model="criterion.checked" :label="criterion.text" />
              <button class="remove-criterion" aria-label="移除验收项" @click="removeCriterion(draft, index)"><el-icon><Close /></el-icon></button>
            </div>
          </div>
          <div v-else class="rail-empty">添加验收项，完成前逐项确认。</div>
          <div v-if="selectedItem.state === 3 && selectedItem.acceptanceState === 'PENDING'" class="acceptance-note">
            <el-icon><CircleCheck /></el-icon><span>验收记录将绑定当前工作项和清单版本。</span>
          </div>
        </section>
        <section class="drawer-section activity-section">
          <div class="section-heading"><h3>变更记录</h3></div>
          <div v-if="activities.length" class="activity-list"><div v-for="activity in activities" :key="activity.id" class="activity-entry"><span class="activity-dot"></span><span><strong>{{ activityAction(activity.action) }}</strong><small>{{ activity.actor }} · {{ activity.createdAt }}</small><small v-if="activity.oldValue || activity.newValue">{{ activity.newValue }}</small></span></div></div>
          <div v-else class="rail-empty">保存工作项后会记录变更。</div>
        </section>
        <div class="drawer-footer">
          <el-button :loading="saving" @click="saveItem">保存修改</el-button>
          <el-button v-if="detailAction" type="primary" :loading="saving" @click="transition(detailAction.action)">{{ detailAction.label }}</el-button>
          <el-button v-if="selectedItem.state === 3 && selectedItem.acceptanceState === 'PENDING'" type="primary" :disabled="!acceptanceComplete" :loading="saving" @click="acceptItem">确认验收</el-button>
        </div>
        <div class="drawer-meta">截止日期：{{ formattedDate(selectedItem.dueDate) }} · 创建于 {{ selectedItem.createdAt.slice(0, 10).replaceAll('-', '/') }}</div>
      </div>
    </template>
  </el-drawer>
</template>
