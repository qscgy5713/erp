<script setup lang="ts">
// 公司資料:統一編號與稅籍編號(營業稅媒體申報檔每筆記錄都要填稅籍編號)
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { systemApi } from '@/api/system'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['system.company.write']))
const { handle, fieldErrors, reset } = useApiError()

const loading = ref(false)
const saving = ref(false)
const form = reactive({ name: '', tax_id: '', tax_reg_no: '', version: 0 })

async function load() {
  loading.value = true
  try {
    Object.assign(form, await systemApi.company())
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function save() {
  reset()
  saving.value = true
  try {
    Object.assign(form, await systemApi.updateCompany({ ...form }))
    ElMessage.success('已儲存')
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <el-card v-loading="loading" shadow="never" style="max-width: 560px">
    <el-form label-width="110px" :disabled="!canWrite" @submit.prevent="save">
      <el-form-item label="公司名稱" :error="fieldErrors.name">
        <el-input v-model="form.name" maxlength="100" />
      </el-form-item>
      <el-form-item label="統一編號" :error="fieldErrors.tax_id">
        <el-input v-model="form.tax_id" maxlength="8" placeholder="8 碼" />
      </el-form-item>
      <el-form-item label="稅籍編號" :error="fieldErrors.tax_reg_no">
        <el-input
          v-model="form.tax_reg_no"
          maxlength="9"
          placeholder="9 碼英數字(營業稅申報書上的稅籍編號)"
        />
      </el-form-item>
      <el-form-item v-if="canWrite">
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </el-form-item>
    </el-form>
  </el-card>
</template>
