<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { systemApi, type DataScope, type PermissionGroup, type Role } from '@/api/system'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { dataScopeLabels, formatDateTime } from '@/utils/format'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('system.role.write'))
const { fieldErrors, handle, reset } = useApiError()

const rows = ref<Role[]>([])
const groups = ref<PermissionGroup[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await systemApi.roles()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  load()
  try {
    groups.value = await systemApi.permissions()
  } catch (e) {
    handle(e)
  }
})

// 非超級管理員只能授予自己擁有的權限(後端也會檢查)
const grantable = (code: string) => auth.can(code)

const dialog = ref(false)
const saving = ref(false)
const editing = ref<Role | null>(null)
const formRef = ref<FormInstance>()
const form = reactive({
  code: '',
  name: '',
  description: '',
  data_scope: 'self' as DataScope,
  permissions: [] as string[],
  is_active: true,
})
const rules: FormRules = {
  code: [{ required: true, message: '必填', trigger: 'blur' }],
  name: [{ required: true, message: '必填', trigger: 'blur' }],
}

function openCreate() {
  editing.value = null
  Object.assign(form, {
    code: '',
    name: '',
    description: '',
    data_scope: 'self',
    permissions: [],
    is_active: true,
  })
  reset()
  dialog.value = true
}

async function openEdit(r: Role) {
  try {
    const full = await systemApi.role(r.id)
    editing.value = full
    Object.assign(form, {
      code: full.code,
      name: full.name,
      description: full.description,
      data_scope: full.data_scope,
      permissions: [...(full.permissions ?? [])],
      is_active: full.is_active,
    })
    reset()
    dialog.value = true
  } catch (e) {
    handle(e)
  }
}

function toggleGroup(g: PermissionGroup, checked: boolean) {
  const codes = g.permissions.map((p) => p.code).filter(grantable)
  const set = new Set(form.permissions)
  for (const c of codes) {
    if (checked) set.add(c)
    else set.delete(c)
  }
  form.permissions = [...set]
}

const groupChecked = (g: PermissionGroup) =>
  g.permissions.every((p) => form.permissions.includes(p.code))
const groupIndeterminate = (g: PermissionGroup) =>
  !groupChecked(g) && g.permissions.some((p) => form.permissions.includes(p.code))

async function save() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    if (editing.value) {
      await systemApi.updateRole(editing.value.id, { ...form, version: editing.value.version })
    } else {
      await systemApi.createRole(form)
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

async function remove(r: Role) {
  try {
    await ElMessageBox.confirm(`確定刪除角色「${r.name}」?`, '刪除角色', {
      type: 'warning',
      confirmButtonText: '刪除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await systemApi.deleteRole(r.id)
    ElMessage.success('已刪除')
    await load()
  } catch (e) {
    handle(e)
  }
}
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openCreate">新增角色</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="code" label="代碼" width="140" />
      <el-table-column prop="name" label="名稱" min-width="140" />
      <el-table-column prop="description" label="說明" min-width="200" show-overflow-tooltip />
      <el-table-column label="資料範圍" width="110">
        <template #default="{ row }">{{ dataScopeLabels[row.data_scope] }}</template>
      </el-table-column>
      <el-table-column prop="user_count" label="人數" width="80" align="right" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'info'">{{
            row.is_active ? '啟用' : '停用'
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="更新時間" width="180">
        <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">編輯</el-button>
          <el-button link type="danger" :disabled="row.user_count > 0" @click="remove(row)"
            >刪除</el-button
          >
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialog"
      :close-on-click-modal="false"
      :title="editing ? '編輯角色' : '新增角色'"
      width="640px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="代碼" prop="code" :error="fieldErrors.code">
          <el-input v-model="form.code" maxlength="30" />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="說明">
          <el-input v-model="form.description" type="textarea" maxlength="500" />
        </el-form-item>
        <el-form-item label="資料範圍" :error="fieldErrors.data_scope">
          <el-radio-group v-model="form.data_scope">
            <el-radio v-for="(label, v) in dataScopeLabels" :key="v" :value="v">{{
              label
            }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item v-if="editing" label="啟用">
          <el-switch v-model="form.is_active" />
        </el-form-item>
        <el-form-item label="權限" :error="fieldErrors.permissions">
          <div class="perm-groups">
            <div v-for="g in groups" :key="g.module + g.resource" class="perm-group">
              <el-checkbox
                :model-value="groupChecked(g)"
                :indeterminate="groupIndeterminate(g)"
                @change="(v: string | number | boolean) => toggleGroup(g, !!v)"
              >
                <strong>{{ g.module }} / {{ g.resource }}</strong>
              </el-checkbox>
              <el-checkbox-group v-model="form.permissions" class="perm-items">
                <el-checkbox
                  v-for="p in g.permissions"
                  :key="p.code"
                  :value="p.code"
                  :disabled="!grantable(p.code)"
                >
                  {{ p.name }}
                </el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
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
.perm-groups {
  width: 100%;
}
.perm-group {
  padding: 6px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.perm-items {
  padding-left: 24px;
}
</style>
