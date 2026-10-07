<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { systemApi, type DocNumberRule } from '@/api/system'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('system.docno.write'))
const { fieldErrors, handle, reset } = useApiError()

const rows = ref<DocNumberRule[]>([])
const loading = ref(false)

const formatLabels: Record<DocNumberRule['date_format'], string> = {
  YYYYMMDD: '年月日(每日重新編號)',
  YYYYMM: '年月(每月重新編號)',
  YYYY: '年(每年重新編號)',
  NONE: '無日期(不重新編號)',
}

async function load() {
  loading.value = true
  try {
    rows.value = await systemApi.docNumberRules()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)

const dialog = ref(false)
const saving = ref(false)
const editing = ref<DocNumberRule | null>(null)
const formRef = ref<FormInstance>()
const form = reactive({
  name: '',
  prefix: '',
  date_format: 'YYYYMMDD' as DocNumberRule['date_format'],
  seq_length: 4,
})
const rules: FormRules = {
  name: [{ required: true, message: '必填', trigger: 'blur' }],
  prefix: [
    { required: true, message: '必填', trigger: 'blur' },
    { pattern: /^[A-Za-z0-9]{1,10}$/, message: '1–10 個英數字', trigger: 'blur' },
  ],
}

// 即時預覽(與後端格式一致)
const preview = computed(() => {
  const d = new Date()
  const y = String(d.getFullYear())
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const date = { YYYYMMDD: y + m + day, YYYYMM: y + m, YYYY: y, NONE: '' }[form.date_format]
  return form.prefix.toUpperCase() + date + '1'.padStart(form.seq_length, '0')
})

function openEdit(r: DocNumberRule) {
  editing.value = r
  Object.assign(form, {
    name: r.name,
    prefix: r.prefix,
    date_format: r.date_format,
    seq_length: r.seq_length,
  })
  reset()
  dialog.value = true
}

async function save() {
  if (!editing.value || !(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    await systemApi.updateDocNumberRule(editing.value.doc_type, {
      ...form,
      version: editing.value.version,
    })
    ElMessage.success('已儲存')
    dialog.value = false
    await load()
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="修改規則只影響之後產生的單號,已建立的單據不會改變。"
      class="mb"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="name" label="單據" min-width="120" />
      <el-table-column prop="prefix" label="前綴" width="100" />
      <el-table-column label="日期格式" min-width="200">
        <template #default="{ row }">{{
          formatLabels[row.date_format as DocNumberRule['date_format']]
        }}</template>
      </el-table-column>
      <el-table-column prop="seq_length" label="流水號位數" width="110" align="right" />
      <el-table-column prop="example" label="範例" min-width="160" />
      <el-table-column v-if="canWrite" label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">編輯</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :close-on-click-modal="false" title="編輯單號規則" width="480px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="單據名稱" prop="name" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="50" />
        </el-form-item>
        <el-form-item label="前綴" prop="prefix" :error="fieldErrors.prefix">
          <el-input v-model="form.prefix" maxlength="10" />
        </el-form-item>
        <el-form-item label="日期格式" :error="fieldErrors.date_format">
          <el-select v-model="form.date_format" style="width: 100%">
            <el-option v-for="(label, v) in formatLabels" :key="v" :label="label" :value="v" />
          </el-select>
        </el-form-item>
        <el-form-item label="流水號位數" :error="fieldErrors.seq_length">
          <el-input-number v-model="form.seq_length" :min="3" :max="10" />
        </el-form-item>
        <el-form-item label="預覽">
          <code>{{ preview }}</code>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
</style>
