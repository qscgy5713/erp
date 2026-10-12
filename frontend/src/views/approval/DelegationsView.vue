<script setup lang="ts">
// 簽核代理人:出差 / 休假期間把「核准」權限交給代理人;代理期間內代理人能核准我負責的單據,並留下「代誰核准」紀錄
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { delegationApi, type Delegation } from '@/api/approval'
import { masterdataApi, type UserOption } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canManageAll = computed(() => auth.can(['system.approval.write']))
const { handle, fieldErrors, reset: clearErrors, autoClear } = useApiError()

const rows = ref<Delegation[]>([])
const users = ref<UserOption[]>([])
const showAll = ref(false)
const loading = ref(false)
const dialog = ref(false)
const saving = ref(false)
const form = reactive({
  delegator_id: null as number | null,
  delegate_id: null as number | null,
  range: [] as string[],
  note: '',
})
autoClear(form)

const today = new Date().toLocaleDateString('sv-SE')
const isActive = (d: Delegation) => d.valid_from <= today && today <= d.valid_to
const statusOf = (d: Delegation) =>
  isActive(d) ? '代理中' : d.valid_from > today ? '尚未生效' : '已結束'

async function load() {
  loading.value = true
  try {
    rows.value = await delegationApi.list(showAll.value && canManageAll.value)
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function openNew() {
  clearErrors()
  Object.assign(form, { delegator_id: null, delegate_id: null, range: [today, today], note: '' })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    await delegationApi.create({
      delegator_id: form.delegator_id ?? undefined,
      delegate_id: form.delegate_id ?? 0,
      valid_from: form.range?.[0] ?? '',
      valid_to: form.range?.[1] ?? '',
      note: form.note,
    })
    ElMessage.success('已設定代理人')
    dialog.value = false
    await load()
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}

async function remove(d: Delegation) {
  try {
    await ElMessageBox.confirm('確定終止這筆代理?代理人將立即失去代理權限。', '終止代理', {
      type: 'warning',
      confirmButtonText: '終止',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await delegationApi.remove(d.id)
    ElMessage.success('已終止')
    await load()
  } catch (e) {
    handle(e)
  }
}

onMounted(async () => {
  try {
    users.value = await masterdataApi.userOptions()
  } catch (e) {
    handle(e)
  }
  await load()
})
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="hint">
        代理期間內,代理人可核准你有核准權限、或負責角色的單據(資料範圍仍以代理人自己為準);同一個人與其代理人不可核准同一張單據的多個層級。
      </span>
      <span class="spacer" />
      <el-checkbox v-if="canManageAll" v-model="showAll" @change="load">顯示全公司</el-checkbox>
      <el-button type="primary" @click="openNew">設定代理人</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border empty-text="尚無代理設定">
      <el-table-column prop="delegator_name" label="委託人" width="160" />
      <el-table-column prop="delegate_name" label="代理人" width="160" />
      <el-table-column label="代理期間" width="230">
        <template #default="{ row }">{{ row.valid_from }} ~ {{ row.valid_to }}</template>
      </el-table-column>
      <el-table-column label="狀態" width="100">
        <template #default="{ row }">
          <el-tag :type="isActive(row) ? 'success' : 'info'" effect="plain">{{
            statusOf(row)
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="note" label="備註" min-width="160" />
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button v-if="row.valid_to >= today" link type="danger" @click="remove(row)">
            終止
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" title="設定簽核代理人" width="480px">
      <el-form label-width="90px">
        <el-form-item v-if="canManageAll" label="委託人" :error="fieldErrors.delegator_id">
          <el-select v-model="form.delegator_id" clearable placeholder="預設為本人" filterable>
            <el-option
              v-for="u in users"
              :key="u.id"
              :label="`${u.name} (${u.username})`"
              :value="u.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="代理人" :error="fieldErrors.delegate_id">
          <el-select v-model="form.delegate_id" placeholder="選擇代理人" filterable>
            <el-option
              v-for="u in users.filter((x) => x.id !== auth.user?.id)"
              :key="u.id"
              :label="`${u.name} (${u.username})`"
              :value="u.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="代理期間" :error="fieldErrors.valid_from || fieldErrors.valid_to">
          <el-date-picker
            v-model="form.range"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="至"
            start-placeholder="起"
            end-placeholder="迄"
          />
        </el-form-item>
        <el-form-item label="備註" :error="fieldErrors.note">
          <el-input v-model="form.note" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>
