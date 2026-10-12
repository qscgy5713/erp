<script setup lang="ts">
// 簽核規則:單據金額(本位幣含稅)達門檻時,除了第 1 層(具核准權限的人)之外,還須依序由指定角色再核准
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  approvalApi,
  type ApprovalMode,
  type ApprovalRule,
  type DocTypeOption,
  type RuleStep,
} from '@/api/approval'
import { systemApi, type Role } from '@/api/system'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['system.approval.write']))
const { handle, fieldErrors, reset: clearErrors, autoClear } = useApiError()

const rules = ref<ApprovalRule[]>([])
const docTypes = ref<DocTypeOption[]>([])
const roles = ref<Role[]>([])
const loading = ref(false)

const dialog = ref(false)
const saving = ref(false)
const editing = ref<ApprovalRule | null>(null)
interface FormStep {
  role_ids: number[]
  mode: ApprovalMode
}
const form = reactive({ doc_type: '', min_amount: '0', steps: [] as FormStep[] })

/** 規則的一層顯示成「財務 或 法務(或簽)」「財務 + 法務(會簽)」 */
function stepText(s: RuleStep): string {
  const names = s.roles.map((r) => r.role_name)
  if (names.length < 2) return names[0] ?? ''
  return s.mode === 'all' ? `${names.join(' + ')}(會簽)` : `${names.join(' 或 ')}(或簽)`
}

const money = (v: string) => Number(v).toLocaleString('zh-TW')

async function load() {
  loading.value = true
  try {
    ;[rules.value, docTypes.value, roles.value] = await Promise.all([
      approvalApi.rules(),
      approvalApi.docTypes(),
      auth.can(['system.role.read', 'system.approval.write'])
        ? systemApi.roles()
        : Promise.resolve([] as Role[]),
    ])
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function open(rule: ApprovalRule | null) {
  clearErrors()
  editing.value = rule
  form.doc_type = rule?.doc_type ?? docTypes.value[0]?.key ?? ''
  form.min_amount = rule?.min_amount ?? '0'
  form.steps = rule
    ? rule.steps.map((s) => ({ role_ids: s.roles.map((r) => r.role_id), mode: s.mode }))
    : [{ role_ids: [], mode: 'any' }]
  dialog.value = true
}

async function save() {
  clearErrors()
  saving.value = true
  const input = {
    doc_type: form.doc_type,
    min_amount: String(form.min_amount),
    steps: form.steps
      .filter((s) => s.role_ids.length > 0)
      .map((s) => ({ role_ids: s.role_ids, mode: s.role_ids.length > 1 ? s.mode : 'any' })),
    version: editing.value?.version,
  }
  try {
    if (editing.value) await approvalApi.updateRule(editing.value.id, input)
    else await approvalApi.createRule(input)
    ElMessage.success('已儲存')
    dialog.value = false
    await load()
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}

async function remove(rule: ApprovalRule) {
  try {
    await ElMessageBox.confirm(
      `刪除「${rule.doc_label} ≥ ${money(rule.min_amount)}」規則?已送審的單據仍依原本的流程簽核。`,
      '刪除簽核規則',
      { type: 'warning', confirmButtonText: '刪除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await approvalApi.deleteRule(rule.id)
    ElMessage.success('已刪除')
    await load()
  } catch (e) {
    handle(e)
  }
}

onMounted(load)

autoClear(form)
</script>

<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb"
      title="第 1 層永遠是具該單據「核准」權限的人;規則可再追加第 2 層以後的角色。每一層可指定多個角色:或簽 = 任一角色核准即通過;會簽 = 每個角色都要有人核准。同一個人不可核准同一張單據的兩個層級或兩格。沒有適用規則的單據維持單層核准。"
    />
    <div class="page-toolbar">
      <el-button v-if="canWrite" type="primary" @click="open(null)">新增規則</el-button>
    </div>
    <el-table v-loading="loading" :data="rules" border>
      <el-table-column prop="doc_label" label="單據" width="170" />
      <el-table-column label="金額門檻(含稅,本位幣)" width="200" align="right">
        <template #default="{ row }">≥ {{ money(row.min_amount) }}</template>
      </el-table-column>
      <el-table-column label="簽核流程" min-width="320">
        <template #default="{ row }">
          <span>第 1 層 核准權限</span>
          <span v-for="s in row.steps" :key="s.step"> → 第 {{ s.step }} 層 {{ stepText(s) }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="120">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">修改</el-button>
          <el-button link type="danger" @click="remove(row)">刪除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="editing ? '修改簽核規則' : '新增簽核規則'" width="620px">
      <el-form label-width="110px" @submit.prevent="save">
        <el-form-item label="單據" :error="fieldErrors.doc_type">
          <el-select v-model="form.doc_type" style="width: 100%">
            <el-option v-for="d in docTypes" :key="d.key" :label="d.label" :value="d.key" />
          </el-select>
        </el-form-item>
        <el-form-item label="金額門檻" :error="fieldErrors.min_amount">
          <el-input v-model="form.min_amount" placeholder="含稅金額達此數字才適用" />
        </el-form-item>
        <el-form-item label="第 1 層">
          <span class="muted">具該單據核准權限的人</span>
        </el-form-item>
        <el-form-item
          v-for="(st, i) in form.steps"
          :key="i"
          :label="`第 ${i + 2} 層`"
          :error="i === 0 ? fieldErrors.role_ids : undefined"
        >
          <div class="step">
            <el-select
              v-model="st.role_ids"
              multiple
              placeholder="選擇一個或多個角色"
              style="width: 300px"
            >
              <el-option
                v-for="r in roles.filter((x) => x.is_active)"
                :key="r.id"
                :label="r.name"
                :value="r.id"
              />
            </el-select>
            <el-radio-group v-if="st.role_ids.length > 1" v-model="st.mode" size="small">
              <el-radio-button value="any">或簽</el-radio-button>
              <el-radio-button value="all">會簽</el-radio-button>
            </el-radio-group>
            <el-button
              v-if="form.steps.length > 1"
              link
              type="danger"
              @click="form.steps.splice(i, 1)"
            >
              移除
            </el-button>
          </div>
        </el-form-item>
        <el-form-item v-if="form.steps.length < 4">
          <el-button link type="primary" @click="form.steps.push({ role_ids: [], mode: 'any' })"
            >+ 再加一層</el-button
          >
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
.step {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.muted {
  color: var(--el-text-color-secondary);
}
</style>
