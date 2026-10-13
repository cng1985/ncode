<script setup>
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { api } from '../api'

const props = defineProps({
  search: { type: String, default: '' },
})

const services = ref([])
const logs = ref([])
const loading = ref(true)
const loadError = ref('')
const selectedId = ref(null)
const action = ref('')
const dialogOpen = ref(false)
const creating = ref(false)
const slugTouched = ref(false)
const logPane = ref(null)
const stickToBottom = ref(true)
const form = reactive({ name: '', code: '', description: '', kind: 'builtin', command: 'heartbeat', argsText: '', workDir: '' })

let pollTimer = 0
let serial = 0

const selected = computed(() => services.value.find((service) => service.id === selectedId.value) || null)
const visibleServices = computed(() => {
  const keyword = props.search.trim().toLocaleLowerCase()
  if (!keyword) return services.value
  return services.value.filter((service) => `${service.name} ${service.code} ${service.description} ${service.command}`.toLocaleLowerCase().includes(keyword))
})

function makeSlug(value) {
  const slug = value.normalize('NFKD').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
  return slug || `service-${Date.now()}`
}

function resetForm() {
  Object.assign(form, { name: '', code: '', description: '', kind: 'builtin', command: 'heartbeat', argsText: '', workDir: '' })
  slugTouched.value = false
}

function updateSlug() {
  if (!slugTouched.value) form.code = makeSlug(form.name)
}

function onKindChange(kind) {
  if (kind === 'builtin' && form.command !== 'heartbeat' && form.command !== 'once') form.command = 'heartbeat'
  if (kind === 'process' && (form.command === 'heartbeat' || form.command === 'once')) form.command = ''
}

function statusClass(state) {
  if (state === 1) return 'status-progress'
  if (state === 2) return 'status-failed'
  return 'status-default'
}

function commandLabel(service) {
  if (service.kind === 'builtin') return service.command === 'heartbeat' ? '内置心跳' : '内置短任务'
  const args = service.args?.length ? ` ${service.args.join(' ')}` : ''
  return `${service.command}${args}`
}

function streamLabel(stream) {
  if (stream === 'stderr') return '错误'
  if (stream === 'system') return '系统'
  return '输出'
}

function replaceService(service) {
  const index = services.value.findIndex((item) => item.id === service.id)
  if (index >= 0) services.value.splice(index, 1, service)
  else services.value.unshift(service)
}

async function loadServices() {
  loading.value = true
  loadError.value = ''
  try {
    const page = await api.services()
    services.value = page.data
    if (selectedId.value && !services.value.some((service) => service.id === selectedId.value)) selectedId.value = null
    if (!selectedId.value && services.value.length) await selectService(services.value[0].id)
  } catch (error) {
    loadError.value = error.message
  } finally {
    loading.value = false
  }
}

async function pullLogs(reset = false) {
  const id = selectedId.value
  if (!id) return
  const token = ++serial
  const afterId = reset || !logs.value.length ? 0 : logs.value[logs.value.length - 1].id
  const [logResult, serviceResult] = await Promise.all([api.serviceLogs(id, afterId), api.service(id)])
  if (token !== serial || selectedId.value !== id) return
  logs.value = reset || afterId === 0 ? logResult.data : logs.value.concat(logResult.data)
  replaceService(serviceResult.data)
}

async function selectService(id) {
  if (selectedId.value !== id) {
    selectedId.value = id
    logs.value = []
    stickToBottom.value = true
  }
  try {
    await pullLogs(true)
  } catch (error) {
    ElMessage.error(error.message)
  }
}

function onLogScroll() {
  const pane = logPane.value
  if (!pane) return
  stickToBottom.value = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 48
}

async function startSelected() {
  if (!selected.value || selected.value.state === 1) return
  action.value = 'start'
  try {
    const result = await api.startService({ id: selected.value.id, expectedVersion: selected.value.version })
    replaceService(result.data)
    await pullLogs(true)
    if (result.data.state === 1) ElMessage.success('服务已启动')
    else if (result.data.state === 2) ElMessage.warning('服务已退出，请查看日志')
    else ElMessage.success('服务已执行完成')
  } catch (error) {
    ElMessage.error(error.message)
    await loadServices()
  } finally {
    action.value = ''
  }
}

async function stopSelected() {
  if (!selected.value || selected.value.state !== 1) return
  action.value = 'stop'
  try {
    const result = await api.stopService({ id: selected.value.id, expectedVersion: selected.value.version })
    replaceService(result.data)
    await pullLogs(true)
    ElMessage.success('服务已停止')
  } catch (error) {
    ElMessage.error(error.message)
    await loadServices()
  } finally {
    action.value = ''
  }
}

function openDialog() {
  resetForm()
  dialogOpen.value = true
}

async function createService() {
  const args = form.kind === 'process' ? form.argsText.split('\n').map((item) => item.trim()).filter(Boolean) : []
  creating.value = true
  try {
    const result = await api.createService({
      name: form.name.trim(),
      code: form.code.trim(),
      description: form.description.trim(),
      kind: form.kind,
      command: form.command.trim(),
      args,
      workDir: form.kind === 'process' ? form.workDir.trim() : '',
    })
    dialogOpen.value = false
    await loadServices()
    await selectService(result.data.id)
    ElMessage.success('服务已登记')
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    creating.value = false
  }
}

function startPoll() {
  stopPoll()
  pollTimer = window.setInterval(() => {
    pullLogs(false).catch(() => {})
  }, 2000)
}

function stopPoll() {
  if (pollTimer) window.clearInterval(pollTimer)
  pollTimer = 0
}

watch(() => logs.value.length, async () => {
  await nextTick()
  if (stickToBottom.value && logPane.value) logPane.value.scrollTop = logPane.value.scrollHeight
})

onMounted(async () => {
  await loadServices()
  startPoll()
})
onUnmounted(stopPoll)
</script>

<template>
  <main class="service-layout">
    <section class="service-list-pane">
      <div class="page-title-row">
        <div>
          <h1>系统服务</h1>
          <p>启动、停止服务，并查看运行日志。</p>
        </div>
        <el-button type="primary" class="primary-action" @click="openDialog"><el-icon><Plus /></el-icon>登记服务</el-button>
      </div>
      <div v-if="loadError" class="service-inline-error" role="alert">
        <span>{{ loadError }}</span>
        <button type="button" class="inline-link" @click="loadServices">重试</button>
      </div>
      <div v-if="visibleServices.length" class="service-list">
        <button v-for="service in visibleServices" :key="service.id" class="service-row" :class="{ selected: service.id === selectedId }" @click="selectService(service.id)">
          <span class="service-mark" :class="statusClass(service.state)">{{ service.kind === 'builtin' ? '内' : '进' }}</span>
          <span class="project-list-copy">
            <strong>{{ service.name }}</strong>
            <small>{{ commandLabel(service) }}</small>
            <small>{{ service.code }} · {{ service.kindName }}</small>
          </span>
          <span class="status-text" :class="statusClass(service.state)">{{ service.stateName }}</span>
        </button>
      </div>
      <div v-else-if="!loading" class="project-empty">
        <strong>{{ search.trim() ? '没有匹配的服务' : '还没有服务' }}</strong>
        <span>可以先启动内置心跳，确认启动、停止和日志查看。</span>
        <el-button v-if="!search.trim()" type="primary" @click="openDialog">登记第一个服务</el-button>
      </div>
      <div v-else class="project-loading" v-loading="loading"></div>
    </section>

    <section class="service-detail-pane">
      <template v-if="selected">
        <div class="detail-title-row">
          <div>
            <h2>{{ selected.name }}</h2>
            <p>{{ selected.description || '这个服务还没有说明。' }}</p>
          </div>
          <div class="service-actions">
            <el-button type="primary" :disabled="selected.state === 1" :loading="action === 'start'" @click="startSelected">启动</el-button>
            <el-button type="danger" plain :disabled="selected.state !== 1" :loading="action === 'stop'" @click="stopSelected">停止</el-button>
          </div>
        </div>
        <div class="service-meta">
          <span class="status-text" :class="statusClass(selected.state)">{{ selected.stateName }}</span>
          <span>{{ commandLabel(selected) }}</span>
          <span v-if="selected.state === 1 && selected.pid">PID {{ selected.pid }}</span>
          <span v-if="selected.exitCode !== null && selected.exitCode !== undefined && selected.state !== 1">退出码 {{ selected.exitCode }}</span>
          <span v-if="selected.workDir">目录 {{ selected.workDir }}</span>
          <span v-if="selected.startedAt">启动于 {{ selected.startedAt }}</span>
        </div>
        <div class="service-log-heading">
          <h3>运行日志</h3>
          <button type="button" class="inline-link" @click="pullLogs(true)"><el-icon><Refresh /></el-icon>刷新</button>
        </div>
        <div ref="logPane" class="service-log" role="log" aria-label="服务日志" @scroll="onLogScroll">
          <template v-if="logs.length">
            <div v-for="entry in logs" :key="entry.id" class="log-line" :class="`log-${entry.stream}`">
              <time>{{ entry.createdAt.slice(11, 23) }}</time>
              <span class="log-stream">{{ streamLabel(entry.stream) }}</span>
              <span class="log-message">{{ entry.message }}</span>
            </div>
          </template>
          <div v-else class="log-line log-placeholder"><span class="log-message">启动服务后，标准输出、错误输出和系统事件会显示在这里。</span></div>
        </div>
      </template>
      <div v-else class="select-project-hint">选择一个服务，查看状态和日志。</div>
    </section>
  </main>

  <el-dialog v-model="dialogOpen" title="登记服务" width="min(560px, calc(100vw - 32px))" destroy-on-close @closed="resetForm">
    <el-form :model="form" label-position="top" @submit.prevent="createService">
      <el-form-item label="服务名称" required><el-input v-model="form.name" maxlength="120" placeholder="例如：演示心跳" @input="updateSlug" /></el-form-item>
      <el-form-item label="服务标识" required><el-input v-model="form.code" maxlength="80" placeholder="demo-heartbeat" @input="slugTouched = true" /></el-form-item>
      <el-form-item label="说明"><el-input v-model="form.description" type="textarea" :rows="2" maxlength="500" placeholder="这个服务负责什么" /></el-form-item>
      <div class="form-two-columns">
        <el-form-item label="类型">
          <el-select v-model="form.kind" @change="onKindChange">
            <el-option label="内置" value="builtin" />
            <el-option label="进程" value="process" />
          </el-select>
        </el-form-item>
        <el-form-item label="命令" required>
          <el-select v-if="form.kind === 'builtin'" v-model="form.command">
            <el-option label="心跳" value="heartbeat" />
            <el-option label="短任务" value="once" />
          </el-select>
          <el-input v-else v-model="form.command" maxlength="200" placeholder="例如：sleep" />
        </el-form-item>
      </div>
      <template v-if="form.kind === 'process'">
        <p class="form-hint">命令会由本机 API 直接执行，不经过 shell。参数每行一个，工作目录需要是已存在的绝对路径。</p>
        <el-form-item label="参数"><el-input v-model="form.argsText" type="textarea" :rows="3" placeholder="每行一个参数" /></el-form-item>
        <el-form-item label="工作目录"><el-input v-model="form.workDir" maxlength="500" placeholder="/tmp" /></el-form-item>
      </template>
      <p v-else class="form-hint">内置心跳会持续写日志，短任务写完两条日志后退出。适合先确认启动、停止和日志查看。</p>
      <div class="dialog-actions">
        <el-button @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" :disabled="!form.name.trim() || !form.code.trim() || !form.command.trim()" @click="createService">登记</el-button>
      </div>
    </el-form>
  </el-dialog>
</template>
