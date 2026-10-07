<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const router = useRouter()
const { fieldErrors, handle } = useApiError()

const formRef = ref<FormInstance>()
const form = reactive({ old_password: '', new_password: '', confirm: '' })
const rules: FormRules = {
  old_password: [{ required: true, message: '請輸入目前密碼', trigger: 'blur' }],
  new_password: [
    { required: true, message: '請輸入新密碼', trigger: 'blur' },
    { min: 8, message: '至少 8 個字元', trigger: 'blur' },
    {
      pattern: /^(?=.*\p{L})(?=.*\d).+$/u,
      message: '需同時包含英文字母與數字',
      trigger: 'blur',
    },
  ],
  confirm: [
    { required: true, message: '請再次輸入新密碼', trigger: 'blur' },
    {
      validator: (_r, v, cb) => (v === form.new_password ? cb() : cb(new Error('兩次輸入不一致'))),
      trigger: 'blur',
    },
  ],
}
const loading = ref(false)

async function submit() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  loading.value = true
  try {
    await auth.changePassword(form.old_password, form.new_password)
    ElMessage.success('密碼已變更,其他裝置的登入已失效')
    router.replace('/')
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <el-card class="card">
    <el-alert
      v-if="auth.mustChangePassword"
      title="首次登入或密碼已被重設,請先變更密碼"
      type="warning"
      :closable="false"
      show-icon
      class="mb"
    />
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
      @submit.prevent="submit"
    >
      <el-form-item label="目前密碼" prop="old_password" :error="fieldErrors.old_password">
        <el-input
          v-model="form.old_password"
          type="password"
          autocomplete="current-password"
          show-password
        />
      </el-form-item>
      <el-form-item label="新密碼" prop="new_password" :error="fieldErrors.new_password">
        <el-input
          v-model="form.new_password"
          type="password"
          autocomplete="new-password"
          show-password
        />
      </el-form-item>
      <el-form-item label="確認新密碼" prop="confirm">
        <el-input
          v-model="form.confirm"
          type="password"
          autocomplete="new-password"
          show-password
        />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" native-type="submit" :loading="loading">變更密碼</el-button>
      </el-form-item>
    </el-form>
  </el-card>
</template>

<style scoped>
.card {
  max-width: 480px;
}
.mb {
  margin-bottom: 16px;
}
</style>
