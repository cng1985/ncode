<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const emit = defineEmits(['authenticated'])
const mode = ref('login')
const busy = ref(false)
const verificationToken = ref('')
const verificationEmail = ref('')
const form = reactive({ name: '', email: '', password: '' })
const heading = computed(() => ({ login: '欢迎回来', register: '创建你的 NCode 账号', verify: '验证邮箱' })[mode.value])

function switchMode(next) {
  mode.value = next
  form.password = ''
}

async function submit() {
  if (busy.value) return
  busy.value = true
  try {
    if (mode.value === 'register') {
      const result = await api.register({ ...form })
      verificationEmail.value = result.data.email
      verificationToken.value = result.data.developmentVerificationToken || ''
      mode.value = 'verify'
      ElMessage.success('账号已创建，请完成邮箱验证')
      return
    }
    if (mode.value === 'verify') {
      const result = await api.verifyEmail(verificationToken.value.trim())
      emit('authenticated', result.data)
      return
    }
    const result = await api.login({ email: form.email, password: form.password })
    emit('authenticated', result.data)
  } catch (error) {
    ElMessage.error(error.message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-card" aria-labelledby="auth-title">
      <div class="brand-lockup"><span class="brand-symbol">N</span><span>NCode</span></div>
      <div class="auth-copy">
        <p class="auth-eyebrow">项目协作与交付平台</p>
        <h1 id="auth-title">{{ heading }}</h1>
        <p v-if="mode === 'login'">登录后继续推进你的项目与工作项。</p>
        <p v-else-if="mode === 'register'">注册后会创建一个仅你可访问的个人空间。</p>
        <p v-else>验证 {{ verificationEmail }} 后即可开始使用。</p>
      </div>

      <el-form v-if="mode !== 'verify'" :model="form" label-position="top" @submit.prevent="submit">
        <el-form-item v-if="mode === 'register'" label="你的名字" required>
          <el-input v-model="form.name" maxlength="80" autocomplete="name" placeholder="怎么称呼你？" />
        </el-form-item>
        <el-form-item label="邮箱" required>
          <el-input v-model="form.email" type="email" maxlength="254" autocomplete="email" placeholder="name@example.com" />
        </el-form-item>
        <el-form-item label="密码" required>
          <el-input v-model="form.password" type="password" show-password autocomplete="current-password" placeholder="至少 12 个字符" @keyup.enter="submit" />
        </el-form-item>
        <el-button class="auth-submit" type="primary" :loading="busy" :disabled="!form.email || !form.password || (mode === 'register' && !form.name.trim())" @click="submit">
          {{ mode === 'login' ? '登录' : '创建账号' }}
        </el-button>
      </el-form>

      <el-form v-else label-position="top" @submit.prevent="submit">
        <el-form-item label="邮箱验证令牌" required>
          <el-input v-model="verificationToken" type="textarea" :rows="3" autocomplete="one-time-code" placeholder="粘贴邮箱收到的验证链接或令牌" />
        </el-form-item>
        <p class="auth-hint">本地开发模式会在此处预填验证令牌。部署环境应配置邮件发送服务。</p>
        <el-button class="auth-submit" type="primary" :loading="busy" :disabled="!verificationToken.trim()" @click="submit">验证并继续</el-button>
      </el-form>

      <div v-if="mode !== 'verify'" class="auth-switch">
        <span>{{ mode === 'login' ? '还没有账号？' : '已经有账号？' }}</span>
        <button type="button" @click="switchMode(mode === 'login' ? 'register' : 'login')">{{ mode === 'login' ? '立即注册' : '返回登录' }}</button>
      </div>
      <button v-else class="auth-back" type="button" @click="switchMode('login')">返回登录</button>
    </section>
    <p class="auth-footnote">你的私有项目和工作项保存在当前工作空间中。</p>
  </main>
</template>
