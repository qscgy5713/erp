<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { systemApi, type Department, type Role, type User } from '@/api/system'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'
import { buildTree } from '@/utils/tree'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('system.user.write'))
const { fieldErrors, handle, reset } = useApiError()

const query = reactive({
  keyword: '',
  department_id: null as number | null,
  is_active: null as boolean | null,
  page: 1,
  size: 20,
})
const rows = ref<User[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)
const departments = ref<Department[]>([])
const roles = ref<Role[]>([])
const deptTree = computed(() => buildTree(departments.value.filter((d) => d.is_active)))

async function load() {
  loading.value = true
  try {
    const res = await systemApi.users({ ...query, keyword: query.keyword.trim() })
    rows.value = res.items
    meta.value = res.meta
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

onMounted(async () => {
  load()
  try {
    ;[departments.value, roles.value] = await Promise.all([
      systemApi.departments(),
      systemApi.roles(),
    ])
  } catch (e) {
    handle(e)
  }
})

const isLocked = (u: User) => !!u.locked_until && new Date(u.locked_until) > new Date()

// ---- 新增/編輯 ----
const dialog = ref(false)
const saving = ref(false)
const editing = ref<User | null>(null)
const formRef = ref<FormInstance>()
const form = reactive({
  username: '',
  name: '',
  email: '',
  department_id: null as number | null,
  password: '',
  role_ids: [] as number[],
  is_active: true,
})
const rules = computed<FormRules>(() => ({
  username: editing.value
    ? []
    : [
        { required: true, message: '必填', trigger: 'blur' },
        { pattern: /^[A-Za-z0-9._-]{3,50}$/, message: '英數字與 . _ -,長度 3–50', trigger: 'blur' },
      ],
  name: [{ required: true, message: '必填', trigger: 'blur' }],
  email: [{ type: 'email', message: 'Email 格式錯誤', trigger: 'blur' }],
  password: editing.value
    ? []
    : [
        { required: true, message: '必填', trigger: 'blur' },
        { min: 8, message: '至少 8 個字元', trigger: 'blur' },
      ],
}))

function openCreate() {
  editing.value = null
  Object.assign(form, {
    username: '',
    name: '',
    email: '',
    department_id: null,
    password: '',
    role_ids: [],
    is_active: true,
  })
  reset()
  dialog.value = true
}

function openEdit(u: User) {
  editing.value = u
  Object.assign(form, {
    username: u.username,
    name: u.name,
    email: u.email ?? '',
    department_id: u.department_id,
    password: '',
    role_ids: [...u.role_ids],
    is_active: u.is_active,
  })
  reset()
  dialog.value = true
}

async function save() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  const email = form.email.trim() || null
  try {
    if (editing.value) {
      await systemApi.updateUser(editing.value.id, {
        name: form.name,
        email,
        department_id: form.department_id,
        is_active: form.is_active,
        role_ids: form.role_ids,
        version: editing.value.version,
      })
    } else {
      await systemApi.createUser({
        username: form.username,
        name: form.name,
        email,
        department_id: form.department_id,
        password: form.password,
        role_ids: form.role_ids,
      })
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

// ---- 重設密碼、解鎖 ----
async function resetPassword(u: User) {
  let password: string
  try {
    const res = await ElMessageBox.prompt(
      `為 ${u.name}(${u.username})設定新密碼,對方下次登入須變更密碼`,
      '重設密碼',
      {
        inputType: 'password',
        inputPattern: /^(?=.*\p{L})(?=.*\d).{8,}$/u,
        inputErrorMessage: '至少 8 個字元,需包含英文字母與數字',
        confirmButtonText: '重設',
        cancelButtonText: '取消',
      },
    )
    password = res.value
  } catch {
    return
  }
  try {
    await systemApi.resetPassword(u.id, password)
    ElMessage.success('密碼已重設,該使用者的登入已全部失效')
  } catch (e) {
    handle(e)
  }
}

async function resetTwoFactor(u: User) {
  try {
    await ElMessageBox.confirm(
      `重設 ${u.name}(${u.username})的雙因素驗證?對方的備援碼會作廢、既有登入全部失效,之後只需密碼即可登入(公司若要求雙因素驗證,須重新設定)。`,
      '重設雙因素驗證',
      { type: 'warning', confirmButtonText: '重設', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await systemApi.resetTwoFactor(u.id)
    ElMessage.success('已重設雙因素驗證')
    await load()
  } catch (e) {
    handle(e)
  }
}

async function unlock(u: User) {
  try {
    await systemApi.unlockUser(u.id)
    ElMessage.success('已解除鎖定')
    await load()
  } catch (e) {
    handle(e)
  }
}
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-input
        v-model="query.keyword"
        placeholder="帳號或姓名"
        clearable
        style="width: 200px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-tree-select
        v-model="query.department_id"
        :data="buildTree(departments)"
        :props="{ label: 'name', children: 'children' }"
        node-key="id"
        check-strictly
        clearable
        placeholder="部門"
        style="width: 180px"
        @change="search"
      />
      <el-select
        v-model="query.is_active"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option label="啟用" :value="true" />
        <el-option label="停用" :value="false" />
      </el-select>
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openCreate">新增使用者</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="username" label="帳號" min-width="120" />
      <el-table-column prop="name" label="姓名" min-width="120" />
      <el-table-column prop="department_name" label="部門" min-width="120" />
      <el-table-column label="角色" min-width="160">
        <template #default="{ row }">
          <el-tag v-if="row.is_superadmin" type="danger" class="tag">超級管理員</el-tag>
          <el-tag v-for="r in row.roles" :key="r.id" class="tag">{{ r.name }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="狀態" width="140">
        <template #default="{ row }">
          <el-tag :type="row.is_active ? 'success' : 'info'" class="tag">{{
            row.is_active ? '啟用' : '停用'
          }}</el-tag>
          <el-tag v-if="isLocked(row)" type="danger" class="tag">鎖定</el-tag>
          <el-tag v-if="row.two_factor_enabled" type="primary" effect="plain" class="tag"
            >2FA</el-tag
          >
        </template>
      </el-table-column>
      <el-table-column label="最後登入" width="180">
        <template #default="{ row }">{{ formatDateTime(row.last_login_at) }}</template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="290" fixed="right">
        <template #default="{ row }">
          <template v-if="!row.is_superadmin || auth.user?.is_superadmin">
            <el-button link type="primary" @click="openEdit(row)">編輯</el-button>
            <el-button link type="primary" @click="resetPassword(row)">重設密碼</el-button>
            <el-button v-if="isLocked(row)" link type="danger" @click="unlock(row)">解鎖</el-button>
            <el-button
              v-if="row.two_factor_enabled"
              link
              type="danger"
              @click="resetTwoFactor(row)"
            >
              重設 2FA
            </el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>

    <el-dialog
      v-model="dialog"
      :close-on-click-modal="false"
      :title="editing ? '編輯使用者' : '新增使用者'"
      width="520px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="帳號" prop="username" :error="fieldErrors.username">
          <el-input
            v-model="form.username"
            :disabled="!!editing"
            maxlength="50"
            autocomplete="off"
          />
        </el-form-item>
        <el-form-item label="姓名" prop="name" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="Email" prop="email" :error="fieldErrors.email">
          <el-input v-model="form.email" maxlength="255" />
        </el-form-item>
        <el-form-item label="部門" :error="fieldErrors.department_id">
          <el-tree-select
            v-model="form.department_id"
            :data="deptTree"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            check-strictly
            clearable
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item
          v-if="!editing"
          label="初始密碼"
          prop="password"
          :error="fieldErrors.password"
        >
          <el-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="角色" :error="fieldErrors.role_ids">
          <el-select v-model="form.role_ids" multiple style="width: 100%">
            <el-option
              v-for="r in roles"
              :key="r.id"
              :label="r.name"
              :value="r.id"
              :disabled="!r.is_active"
            />
          </el-select>
        </el-form-item>
        <el-form-item v-if="editing" label="啟用" :error="fieldErrors.is_active">
          <el-switch v-model="form.is_active" :disabled="editing.id === auth.user?.id" />
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
.tag {
  margin-right: 4px;
}
</style>
