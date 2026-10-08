<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { ApiRequestError } from '@/api/http'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const formRef = ref<FormInstance>()
const form = reactive({ username: '', password: '' })
const rules: FormRules = {
  username: [{ required: true, message: '請輸入帳號', trigger: 'blur' }],
  password: [{ required: true, message: '請輸入密碼', trigger: 'blur' }],
}
const loading = ref(false)
const error = ref('')
// 已啟用雙因素驗證的帳號:密碼正確後進入第二步,輸入驗證器 App 的 6 位數(或備援碼)
const challenge = ref<string | null>(null)
const code = ref('')

/** 只允許站內相對路徑,避免開放式重新導向 */
function safeRedirect(v: unknown): string {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') ? v : '/'
}

async function submit() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  loading.value = true
  error.value = ''
  try {
    const ch = await auth.login(form.username, form.password)
    if (ch) {
      challenge.value = ch
      return
    }
    done()
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : '登入失敗,請稍後再試'
    form.password = ''
  } finally {
    loading.value = false
  }
}
function done() {
  router.replace(
    auth.mustChangePassword
      ? { name: 'change-password' }
      : auth.mustSetup2FA
        ? { name: 'account-security' }
        : safeRedirect(route.query.redirect),
  )
}

async function submitCode() {
  if (!challenge.value || !code.value.trim()) return
  loading.value = true
  error.value = ''
  try {
    await auth.loginTwoFactor(challenge.value, code.value)
    done()
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : '驗證失敗,請稍後再試'
    code.value = ''
    // 挑戰逾時(5 分鐘)或帳號被鎖定:回到第一步重新開始
    if (e instanceof ApiRequestError && (e.code === 'AUTH-009' || e.status === 423)) {
      challenge.value = null
      form.password = ''
    }
  } finally {
    loading.value = false
  }
}

function backToPassword() {
  challenge.value = null
  code.value = ''
  error.value = ''
  form.password = ''
}
</script>

<template>
  <div class="login-page">
    <el-card class="login-card">
      <h1>ERP 登入</h1>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="mb" />
      <el-form v-if="challenge" label-position="top" @submit.prevent="submitCode">
        <p class="hint">
          請輸入驗證器 App 目前顯示的 6 位數驗證碼。手機不在身邊時,可改輸入一組備援碼。
        </p>
        <el-form-item label="驗證碼或備援碼">
          <el-input
            v-model="code"
            autocomplete="one-time-code"
            inputmode="text"
            maxlength="20"
            autofocus
            placeholder="123456"
          />
        </el-form-item>
        <el-button type="primary" native-type="submit" :loading="loading" class="full"
          >驗證並登入</el-button
        >
        <el-button link class="back" @click="backToPassword">← 重新輸入帳號密碼</el-button>
      </el-form>
      <el-form
        v-else
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        @submit.prevent="submit"
      >
        <el-form-item label="帳號" prop="username">
          <el-input v-model="form.username" autocomplete="username" autofocus />
        </el-form-item>
        <el-form-item label="密碼" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            autocomplete="current-password"
            show-password
          />
        </el-form-item>
        <el-button type="primary" native-type="submit" :loading="loading" class="full"
          >登入</el-button
        >
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-light);
  padding: 16px;
  box-sizing: border-box;
}
.login-card {
  width: 100%;
  max-width: 360px;
}
h1 {
  font-size: 20px;
  margin: 0 0 16px;
  text-align: center;
}
.mb {
  margin-bottom: 16px;
}
.full {
  width: 100%;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 12px;
}
.back {
  margin-top: 8px;
}
</style>
