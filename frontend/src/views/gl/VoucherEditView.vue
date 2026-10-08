<script setup lang="ts">
// 傳票:手動傳票的草稿可編輯、過帳、作廢;已過帳可沖銷。自動拋轉的傳票唯讀,由來源單據反過帳沖銷。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { glApi, sourceLabels, type AccountOption, type Voucher, type VoucherLine } from '@/api/gl'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'
import PartnerPicker from '@/components/PartnerPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const doc = ref<Voucher | null>(null)
const isNew = computed(() => !doc.value)
const isManual = computed(() => isNew.value || doc.value?.source_type === 'manual')
const editable = computed(
  () =>
    auth.can(['gl.voucher.write']) &&
    (isNew.value || (isManual.value && doc.value?.status === 'draft')),
)
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)
const accounts = ref<AccountOption[]>([])

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const blank = (): VoucherLine => ({
  account_id: null,
  debit: '',
  credit: '',
  description: '',
  customer_id: null,
  supplier_id: null,
  department_id: null,
})

const form = reactive({
  voucher_date: today(),
  description: '',
  lines: [] as VoucherLine[],
})

function applyDoc(d: Voucher) {
  doc.value = d
  Object.assign(form, {
    voucher_date: d.voucher_date,
    description: d.description,
    lines: d.lines.map((l) => ({
      ...l,
      debit: Number(l.debit) ? l.debit : '',
      credit: Number(l.credit) ? l.credit : '',
    })),
  })
  reset()
  setTimeout(() => (dirty.value = false))
}

watch(form, () => (dirty.value = true), { deep: true })

const num = (v: string | number) => (v === '' || Number.isNaN(Number(v)) ? 0 : Number(v))
const totals = computed(() => {
  const debit = form.lines.reduce((s, l) => s + num(l.debit), 0)
  const credit = form.lines.reduce((s, l) => s + num(l.credit), 0)
  return { debit, credit, diff: debit - credit }
})
const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })

async function load() {
  const id = Number(route.params.id)
  if (!id) return
  loading.value = true
  try {
    applyDoc(await glApi.voucher(id))
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function save(): Promise<boolean> {
  saving.value = true
  try {
    const input = {
      voucher_date: form.voucher_date,
      description: form.description,
      lines: form.lines
        .filter((l) => l.account_id)
        .map((l) => ({
          account_id: l.account_id!,
          debit: l.debit === '' ? '0' : l.debit,
          credit: l.credit === '' ? '0' : l.credit,
          description: l.description,
          customer_id: l.customer_id,
          supplier_id: l.supplier_id,
          department_id: l.department_id,
        })),
      version: doc.value?.version,
    }
    const saved = doc.value
      ? await glApi.updateVoucher(doc.value.id, input)
      : await glApi.createVoucher(input)
    applyDoc(saved)
    ElMessage.success('已儲存')
    if (route.name !== 'gl-voucher')
      router.replace({ name: 'gl-voucher', params: { id: saved.id } })
    return true
  } catch (e) {
    handle(e)
    return false
  } finally {
    saving.value = false
  }
}

// ---- 動作 ----

const acting = ref<string | null>(null)
const canPost = computed(() => auth.can(['gl.voucher.post']))
const canReverse = computed(
  () =>
    canPost.value &&
    isManual.value &&
    doc.value?.status === 'posted' &&
    !doc.value.reversal_of &&
    !doc.value.reversed_by_no,
)

const confirms: Record<string, string> = {
  post: '過帳後傳票不可修改,更正需以沖銷傳票處理,確定過帳?',
  void: '作廢後無法復原,確定作廢?',
  reverse: '將產生一張借貸相反、日期相同的沖銷傳票,確定沖銷?',
}
const labels: Record<string, string> = { post: '過帳', void: '作廢', reverse: '沖銷' }

async function run(action: 'post' | 'void' | 'reverse') {
  if (!doc.value) return
  try {
    await ElMessageBox.confirm(confirms[action]!, labels[action]!, {
      type: 'warning',
      confirmButtonText: labels[action],
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  if (dirty.value && editable.value && !(await save())) return
  acting.value = action
  try {
    applyDoc(await glApi.voucherAction(doc.value.id, action, doc.value.version))
    ElMessage.success(`已${labels[action]}`)
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

/** 自動拋轉傳票的來源單據頁 */
const sourceRoute = computed(() => {
  const d = doc.value
  if (!d?.source_id) return null
  const name: Record<string, string> = {
    goods_receipt: 'purchase-receipt',
    purchase_return: 'purchase-receipt',
    delivery: 'sales-delivery',
    sales_return: 'sales-delivery',
    collection: 'collection',
    payment: 'payment',
  }
  return name[d.source_type] ? { name: name[d.source_type]!, params: { id: d.source_id } } : null
})

onMounted(async () => {
  loading.value = true
  try {
    accounts.value = await glApi.accountOptions()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
  await load()
  if (isNew.value) form.lines = [blank(), blank()]
  setTimeout(() => (dirty.value = false))
})
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: 'gl-vouchers' })">← 返回列表</el-button>
        <h2>傳票 {{ doc?.doc_no ?? '(新傳票)' }}</h2>
        <el-tag
          v-if="doc"
          :type="doc.status === 'posted' ? 'success' : doc.status === 'voided' ? 'danger' : 'info'"
        >
          {{ { draft: '草稿', posted: '已過帳', voided: '已作廢' }[doc.status] }}
        </el-tag>
        <el-tag v-if="doc?.reversal_of_no" type="warning" effect="plain">
          沖銷 {{ doc.reversal_of_no }}
        </el-tag>
        <el-tag v-if="doc?.reversed_by_no" type="warning" effect="plain">
          已被 {{ doc.reversed_by_no }} 沖銷
        </el-tag>
        <el-tag v-if="dirty && editable && doc" type="warning" effect="plain">未儲存</el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">儲存</el-button>
        <el-button
          v-if="doc?.status === 'draft' && isManual && canPost"
          type="success"
          :loading="acting === 'post'"
          @click="run('post')"
        >
          過帳
        </el-button>
        <el-button
          v-if="doc?.status === 'draft' && isManual && auth.can(['gl.voucher.write'])"
          type="danger"
          :loading="acting === 'void'"
          @click="run('void')"
        >
          作廢
        </el-button>
        <el-button
          v-if="canReverse"
          type="danger"
          :loading="acting === 'reverse'"
          @click="run('reverse')"
        >
          沖銷
        </el-button>
      </div>
    </div>

    <el-alert v-if="doc && !isManual" type="info" :closable="false" show-icon class="mb">
      <template #title>
        由{{ sourceLabels[doc.source_type] ?? doc.source_type }}單 {{ doc.source_no }} 自動拋轉。
        <RouterLink v-if="sourceRoute" :to="sourceRoute">查看來源單據</RouterLink>
        更正請由來源單據反過帳,系統會自動沖銷。
      </template>
    </el-alert>

    <el-card shadow="never" class="mb">
      <el-form :model="form" label-width="70px" :disabled="!editable">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="8">
            <el-form-item label="日期" :error="fieldErrors.voucher_date">
              <el-date-picker
                v-model="form.voucher_date"
                value-format="YYYY-MM-DD"
                :clearable="false"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="16">
            <el-form-item label="摘要" :error="fieldErrors.description">
              <el-input v-model="form.description" maxlength="255" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <template #header>
        分錄({{ form.lines.length }} 行)
        <span v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</span>
      </template>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column label="科目" min-width="230">
          <template #default="{ row, $index }">
            <el-select
              v-if="editable"
              v-model="row.account_id"
              filterable
              size="small"
              placeholder="代號 / 名稱"
              style="width: 100%"
            >
              <el-option
                v-for="a in accounts"
                :key="a.id"
                :label="`${a.code} ${a.name}`"
                :value="a.id"
              />
            </el-select>
            <span v-else>{{ row.account_code }} {{ row.account_name }}</span>
            <div v-if="fieldErrors[`lines.${$index}`]" class="err">
              {{ fieldErrors[`lines.${$index}`] }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="借方" width="130" align="right">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.debit" size="small" />
            <span v-else>{{ Number(row.debit) ? money(row.debit) : '' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="貸方" width="130" align="right">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.credit" size="small" />
            <span v-else>{{ Number(row.credit) ? money(row.credit) : '' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="說明" min-width="140">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.description" size="small" maxlength="255" />
            <span v-else>{{ row.description }}</span>
          </template>
        </el-table-column>
        <el-table-column label="客戶" width="170">
          <template #default="{ row }">
            <PartnerPicker
              v-if="editable"
              v-model="row.customer_id"
              kind="customer"
              :label="row.customer_name ?? undefined"
              clearable
            />
            <span v-else>{{ row.customer_name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="供應商" width="170">
          <template #default="{ row }">
            <PartnerPicker
              v-if="editable"
              v-model="row.supplier_id"
              kind="supplier"
              :label="row.supplier_name ?? undefined"
              clearable
            />
            <span v-else>{{ row.supplier_name }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="editable" width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="form.lines.splice($index, 1)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="footer">
        <el-button v-if="editable" @click="form.lines.push(blank())">新增分錄</el-button>
        <span class="spacer" />
        <div class="totals">
          <span>借方合計 {{ money(editable ? totals.debit : (doc?.total_amount ?? 0)) }}</span>
          <span>貸方合計 {{ money(editable ? totals.credit : (doc?.total_amount ?? 0)) }}</span>
          <strong v-if="editable" :class="{ err: totals.diff !== 0 }">
            {{ totals.diff === 0 ? '借貸平衡' : `差額 ${money(totals.diff)}` }}
          </strong>
        </div>
      </div>
    </el-card>

    <el-descriptions v-if="doc" :column="3" border size="small" class="mt">
      <el-descriptions-item label="來源">
        {{ sourceLabels[doc.source_type] ?? doc.source_type }} {{ doc.source_no }}
      </el-descriptions-item>
      <el-descriptions-item label="建立">{{ doc.created_by_name }}</el-descriptions-item>
      <el-descriptions-item label="過帳">
        {{ doc.posted_by_name }} {{ formatDateTime(doc.posted_at) }}
      </el-descriptions-item>
    </el-descriptions>
  </div>
</template>

<style scoped>
.doc-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.title h2 {
  margin: 0;
  font-size: 18px;
}
.mb {
  margin-bottom: 12px;
}
.mt {
  margin-top: 12px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.spacer {
  flex: 1;
}
.totals {
  display: flex;
  gap: 16px;
  align-items: baseline;
}
</style>
