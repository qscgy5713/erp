<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { systemApi, type Department } from '@/api/system'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { buildTree, descendantIds } from '@/utils/tree'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('system.department.write'))
const { fieldErrors, handle, reset } = useApiError()

const list = ref<Department[]>([])
const loading = ref(false)
const tree = computed(() => buildTree(list.value))

async function load() {
  loading.value = true
  try {
    list.value = await systemApi.departments()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)

const dialog = ref(false)
const saving = ref(false)
const editing = ref<Department | null>(null)
const formRef = ref<FormInstance>()
const form = reactive({
  parent_id: null as number | null,
  code: '',
  name: '',
  sort_order: 0,
  is_active: true,
})
const rules: FormRules = {
  code: [{ required: true, message: '必填', trigger: 'blur' }],
  name: [{ required: true, message: '必填', trigger: 'blur' }],
}

// 上層部門選項:編輯時排除自己與下層,避免形成循環
const parentOptions = computed(() => {
  const excluded = editing.value ? descendantIds(list.value, editing.value.id) : new Set<number>()
  return buildTree(list.value.filter((d) => !excluded.has(d.id)))
})

function openCreate(parent?: Department) {
  editing.value = null
  Object.assign(form, {
    parent_id: parent?.id ?? null,
    code: '',
    name: '',
    sort_order: 0,
    is_active: true,
  })
  reset()
  dialog.value = true
}

function openEdit(d: Department) {
  editing.value = d
  Object.assign(form, {
    parent_id: d.parent_id,
    code: d.code,
    name: d.name,
    sort_order: d.sort_order,
    is_active: d.is_active,
  })
  reset()
  dialog.value = true
}

async function save() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    if (editing.value) {
      await systemApi.updateDepartment(editing.value.id, {
        ...form,
        version: editing.value.version,
      })
    } else {
      await systemApi.createDepartment(form)
    }
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
    <div class="page-toolbar">
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openCreate()">新增部門</el-button>
    </div>
    <el-table v-loading="loading" :data="tree" row-key="id" default-expand-all border>
      <el-table-column prop="code" label="代碼" min-width="140" />
      <el-table-column prop="name" label="名稱" min-width="160" />
      <el-table-column prop="user_count" label="人數" width="80" align="right" />
      <el-table-column prop="sort_order" label="排序" width="80" align="right" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'info'">{{
            row.is_active ? '啟用' : '停用'
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">編輯</el-button>
          <el-button link type="primary" @click="openCreate(row)">新增下層</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialog"
      :close-on-click-modal="false"
      :title="editing ? '編輯部門' : '新增部門'"
      width="480px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="上層部門" :error="fieldErrors.parent_id">
          <el-tree-select
            v-model="form.parent_id"
            :data="parentOptions"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            check-strictly
            clearable
            placeholder="(無,為最上層)"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="代碼" prop="code" :error="fieldErrors.code">
          <el-input v-model="form.code" maxlength="20" />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" />
        </el-form-item>
        <el-form-item v-if="editing" label="啟用">
          <el-switch v-model="form.is_active" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
