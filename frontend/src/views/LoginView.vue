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

/** 只允許站內相對路徑,避免開放式重新導向 */
function safeRedirect(v: unknown): string {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') ? v : '/'
}

async function submit() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  loading.value = true
  error.value = ''
  try {
    await auth.login(form.username, form.password)
    router.replace(
      auth.mustChangePassword ? { name: 'change-password' } : safeRedirect(route.query.redirect),
    )
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : '登入失敗,請稍後再試'
    form.password = ''
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <el-card class="login-card">
      <h1>ERP 登入</h1>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="mb" />
      <el-form
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
</style>
