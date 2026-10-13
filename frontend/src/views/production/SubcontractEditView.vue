<script setup lang="ts">
// 委外加工單(D100):發料給加工廠 → 收回成品(可分批)→ 加工費轉應付
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  productionApi,
  type ScKind,
  type ScLine,
  type ScTxn,
  type SubcontractOrder,
} from '@/api/production'
import type { ItemOption } from '@/api/inventory'
import { masterdataApi, type Warehouse } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { scKindLabels, scStatusLabels, scStatusTypes } from '@/utils/subcontract'
import ItemPicker from '@/components/ItemPicker.vue'
import PartnerPicker from '@/components/PartnerPicker.vue'
import SubcontractDialog from '@/components/SubcontractDialog.vue'
import ApprovalProgress from '@/components/ApprovalProgress.vue'
import type { ApprovalProgress as ApprovalProgressData } from '@/api/approval'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset, autoClear } = useApiError()

const doc = ref<SubcontractOrder | null>(null)
const isNew = computed(() => !doc.value)
const isOpen = computed(
  () => isNew.value || ['open', 'draft', 'pending'].includes(doc.value?.status ?? ''),
)
const editable = computed(() => auth.can('production.subcontract.write') && isOpen.value)
const canPost = computed(
  () => auth.can('production.subcontract.post') && doc.value?.status === 'open',
)
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)
const warehouses = ref<Warehouse[]>([])
const activeWarehouses = computed(() => warehouses.value.filter((w) => w.is_active))

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const form = reactive({
  doc_date: today(),
  supplier_id: null as number | null,
  supplier_label: undefined as string | undefined,
  item_id: null as number | null,
  item_label: undefined as string | undefined,
  plan_qty: '',
  unit_fee: '0',
  issue_warehouse_id: null as number | null,
  receive_warehouse_id: null as number | null,
  due_date: null as string | null,
  note: '',
  lines: [] as ScLine[],
})

function applyDoc(d: SubcontractOrder) {
  doc.value = d
  Object.assign(form, {
    doc_date: d.doc_date,
    supplier_id: d.supplier_id,
    supplier_label: `${d.supplier_code} ${d.supplier_name}`,
    item_id: d.item_id,
    item_label: `${d.item_code} ${d.item_name}`,
    plan_qty: d.plan_qty,
    unit_fee: d.unit_fee,
    issue_warehouse_id: d.issue_warehouse_id,
    receive_warehouse_id: d.receive_warehouse_id,
    due_date: d.due_date,
    note: d.note,
    lines: d.lines.map((l) => ({ ...l })),
  })
  reset()
  setTimeout(() => (dirty.value = false))
}

watch(form, () => (dirty.value = true), { deep: true })

async function load() {
  const id = Number(route.params.id)
  if (!id) return
  loading.value = true
  try {
    applyDoc(await productionApi.subcontract(id))
  } catch (e) {
    handle(e)
    router.replace({ name: 'subcontracts' })
  } finally {
    loading.value = false
  }
}

// ---- 預計用量 ----
const manualLines = ref(false)

async function explode(force = false) {
  if (!isNew.value || !form.item_id || !Number(form.plan_qty)) return
  if (manualLines.value && !force) return
  try {
    const lines = await productionApi.explode(form.item_id, form.plan_qty, form.doc_date)
    form.lines = lines.map((l) => ({
      item_id: l.item_id,
      item_code: l.item_code,
      item_name: l.item_name,
      unit_name: l.unit_name,
      qty: l.qty,
      note: '',
    }))
    manualLines.value = false
  } catch (e) {
    // 沒有 BOM 是可預期的狀況:讓使用者自行輸入材料
    if (force) handle(e)
  }
}

function onPickMaterial(row: ScLine, item: ItemOption | null) {
  manualLines.value = true
  if (!item) return
  row.item_code = item.code
  row.item_name = item.name
  row.unit_name = item.base_unit_name
}

function addLine() {
  manualLines.value = true
  form.lines.push({ item_id: null, qty: '', note: '' })
}

const sentIndex = ref<number[]>([])
function lineError(i: number): string | undefined {
  const k = sentIndex.value.indexOf(i)
  return k < 0 ? undefined : fieldErrors.value[`lines.${k}`]
}

function payload() {
  sentIndex.value = form.lines.flatMap((l, i) => (l.item_id !== null ? [i] : []))
  return {
    doc_date: form.doc_date,
    supplier_id: form.supplier_id ?? 0,
    item_id: form.item_id ?? 0,
    plan_qty: form.plan_qty === '' ? '0' : form.plan_qty,
    unit_fee: form.unit_fee === '' ? '0' : form.unit_fee,
    issue_warehouse_id: form.issue_warehouse_id ?? 0,
    receive_warehouse_id: form.receive_warehouse_id ?? 0,
    due_date: form.due_date || null,
    note: form.note,
    lines: form.lines
      .filter((l): l is ScLine & { item_id: number } => l.item_id !== null)
      .map((l) => ({ item_id: l.item_id, qty: l.qty === '' ? '0' : l.qty, note: l.note })),
    version: doc.value?.version,
  }
}

async function save(): Promise<boolean> {
  reset()
  saving.value = true
  try {
    const saved = doc.value
      ? await productionApi.updateSubcontract(doc.value.id, payload())
      : await productionApi.createSubcontract(payload())
    ElMessage.success('已儲存')
    if (isNew.value) {
      dirty.value = false
      router.replace({ name: 'subcontract', params: { id: saved.id } })
    } else {
      applyDoc(saved)
    }
    return true
  } catch (e) {
    handle(e)
    return false
  } finally {
    saving.value = false
  }
}

// ---- 發料 / 收回 / 退料 ----
const dlgMode = ref<ScKind>('issue')
const dlgVisible = ref(false)

async function openDialog(mode: ScKind) {
  if (dirty.value && editable.value && !(await save())) return
  dlgMode.value = mode
  dlgVisible.value = true
}

const acting = ref(false)

async function confirmAnd(
  msg: string,
  title: string,
  run: () => Promise<SubcontractOrder>,
  ok: string,
) {
  try {
    await ElMessageBox.confirm(msg, title, {
      type: 'warning',
      confirmButtonText: title,
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  acting.value = true
  try {
    const next = await run()
    applyDoc(next)
    ElMessage.success(
      ok === '已核准' && next.status === 'pending' ? '已完成本層核准,等待下一層核准' : ok,
    )
    progressRef.value?.reload()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = false
  }
}

const closeOrder = () =>
  doc.value &&
  confirmAnd(
    `已收回 ${doc.value.received_qty} / 預計 ${doc.value.plan_qty},結案後剩餘的預計量不會再收回,確定短交結案?`,
    '短交結案',
    () => productionApi.scClose(doc.value!.id, doc.value!.version),
    '已結案',
  )

const approvalInfo = ref<ApprovalProgressData | null>(null)
const progressRef = ref<InstanceType<typeof ApprovalProgress> | null>(null)
const canApprove = computed(
  () =>
    doc.value?.status === 'pending' &&
    auth.can('production.subcontract.approve') &&
    (!approvalInfo.value || approvalInfo.value.can_approve),
)

const approveOrder = () =>
  doc.value &&
  confirmAnd(
    '確定核准這張委外加工單?',
    '核准',
    () => productionApi.scApprove(doc.value!.id, doc.value!.version),
    '已核准',
  )

const rejectOrder = () =>
  doc.value &&
  confirmAnd(
    '退回後單據變成草稿,需修改後重新送審,確定退回?',
    '退回',
    () => productionApi.scReject(doc.value!.id, doc.value!.version),
    '已退回',
  )

const voidOrder = () =>
  doc.value &&
  confirmAnd(
    '作廢後無法復原,確定作廢這張委外加工單?',
    '作廢',
    () => productionApi.scVoid(doc.value!.id, doc.value!.version),
    '已作廢',
  )

const voidTxn = (t: ScTxn) =>
  doc.value &&
  confirmAnd(
    t.kind === 'receive'
      ? `作廢收回 ${t.doc_no} 會沖銷成品入庫、材料耗用與應付加工費(成品若已被使用或出貨,庫存不足時會失敗),確定?`
      : `作廢${scKindLabels[t.kind]} ${t.doc_no} 會沖銷這筆材料轉移,確定?`,
    '作廢',
    () => productionApi.scVoidTxn(doc.value!.id, t.id),
    `已作廢${scKindLabels[t.kind]}`,
  )

const hasLiveTxn = computed(() => doc.value?.txns.some((t) => t.status === 'posted') ?? false)

onMounted(async () => {
  try {
    warehouses.value = await masterdataApi.normalWarehouses()
  } catch (e) {
    handle(e)
  }
  await load()
  setTimeout(() => (dirty.value = false))
})

autoClear(form)
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: 'subcontracts' })">← 返回列表</el-button>
        <h2>委外加工單 {{ doc?.doc_no ?? '(新單據)' }}</h2>
        <el-tag v-if="doc" :type="scStatusTypes[doc.status]">{{
          scStatusLabels[doc.status]
        }}</el-tag>
        <el-tag v-if="dirty && editable && doc" type="warning" effect="plain">未儲存</el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">儲存</el-button>
        <template v-if="doc && canPost">
          <el-button @click="openDialog('issue')">發料</el-button>
          <el-button type="success" @click="openDialog('receive')">收回</el-button>
          <el-button @click="openDialog('return')">退料</el-button>
        </template>
        <template v-if="doc && canApprove">
          <el-button type="success" :loading="acting" @click="approveOrder">核准</el-button>
          <el-button type="warning" :loading="acting" @click="rejectOrder">退回</el-button>
        </template>
        <el-button
          v-if="doc && doc.can_close && auth.can('production.subcontract.write')"
          :loading="acting"
          @click="closeOrder"
          >短交結案</el-button
        >
        <el-button
          v-if="doc && editable && !hasLiveTxn"
          type="danger"
          :loading="acting"
          @click="voidOrder"
          >作廢</el-button
        >
      </div>
    </div>

    <ApprovalProgress
      ref="progressRef"
      doc-type="subcontract_order"
      :doc-id="doc?.id"
      :status="doc?.status"
      :version="doc?.version"
      @loaded="approvalInfo = $event"
    />
    <el-alert
      v-if="doc?.status === 'pending'"
      type="warning"
      :closable="false"
      show-icon
      title="這張單據適用簽核規則,核准後才能發料與收回"
      class="mb"
    />

    <el-card shadow="never" class="mb">
      <el-form label-width="110px" :disabled="!editable">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-form-item label="加工廠" :error="fieldErrors.supplier_id">
              <PartnerPicker
                v-model="form.supplier_id"
                kind="supplier"
                :label="form.supplier_label"
                :disabled="!editable || !isNew"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-form-item label="成品" :error="fieldErrors.item_id">
              <ItemPicker
                v-model="form.item_id"
                :label="form.item_label"
                item-type="goods"
                :disabled="!editable || !isNew"
                @select="explode()"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="預計收回量" :error="fieldErrors.plan_qty">
              <el-input v-model="form.plan_qty" @change="explode()" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="加工費單價" :error="fieldErrors.unit_fee">
              <el-input v-model="form.unit_fee" placeholder="含稅,每個成品" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="日期" :error="fieldErrors.doc_date">
              <el-date-picker
                v-model="form.doc_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
                :clearable="false"
                @change="explode()"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="預定收回日">
              <el-date-picker
                v-model="form.due_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="發料倉" :error="fieldErrors.issue_warehouse_id">
              <el-select
                v-model="form.issue_warehouse_id"
                style="width: 100%"
                :disabled="!editable || hasLiveTxn"
              >
                <el-option
                  v-for="w in activeWarehouses"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="成品收回倉" :error="fieldErrors.receive_warehouse_id">
              <el-select
                v-model="form.receive_warehouse_id"
                style="width: 100%"
                :disabled="!editable || hasLiveTxn"
              >
                <el-option
                  v-for="w in activeWarehouses"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col v-if="doc" :xs="24" :sm="12" :md="6">
            <el-form-item label="委外倉">
              <el-input :model-value="doc.sc_warehouse_name" disabled />
            </el-form-item>
          </el-col>
          <el-col :span="24">
            <el-form-item label="備註"
              ><el-input v-model="form.note" maxlength="1000"
            /></el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </el-card>

    <el-card shadow="never" class="mb">
      <template #header>
        <div class="card-head">
          預計材料用量({{ form.lines.filter((l) => l.item_id).length }} 筆)
          <el-button v-if="editable && isNew" size="small" @click="explode(true)"
            >依 BOM 重新計算</el-button
          >
        </div>
      </template>
      <div v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</div>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column label="材料" min-width="240">
          <template #default="{ row, $index }">
            <ItemPicker
              v-if="editable"
              v-model="row.item_id"
              :label="row.item_code ? `${row.item_code} ${row.item_name}` : undefined"
              item-type="goods"
              @select="(it: ItemOption | null) => onPickMaterial(row, it)"
            />
            <span v-else>{{ row.item_code }} {{ row.item_name }}</span>
            <div v-if="lineError($index)" class="err">{{ lineError($index) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="預計用量" width="150">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.qty" size="small" @input="manualLines = true" />
            <span v-else>{{ row.qty }}</span>
            <span class="muted"> {{ row.unit_name }}</span>
          </template>
        </el-table-column>
        <template v-if="doc">
          <el-table-column prop="issued_qty" label="累計發料" width="100" align="right" />
          <el-table-column prop="consumed_qty" label="累計耗用" width="100" align="right" />
          <el-table-column label="在加工廠" width="100" align="right">
            <template #default="{ row }"
              ><b :class="{ warn: Number(row.held_qty) > 0 }">{{ row.held_qty }}</b></template
            >
          </el-table-column>
        </template>
        <el-table-column label="備註" min-width="120">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.note" size="small" maxlength="255" />
            <span v-else>{{ row.note }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="editable" width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="form.lines.splice($index, 1)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-button v-if="editable" class="addline" @click="addLine">新增材料</el-button>
    </el-card>

    <el-card v-if="doc" shadow="never" class="mb">
      <template #header>
        <div class="card-head">
          發料 / 收回 / 退料紀錄:已收回 {{ doc.received_qty }} / {{ doc.plan_qty }}
          {{ doc.unit_name }}
        </div>
      </template>
      <el-table :data="doc.txns" border size="small" empty-text="還沒有異動紀錄">
        <el-table-column prop="doc_no" label="單號" width="150" />
        <el-table-column label="類型" width="80">
          <template #default="{ row }">{{ scKindLabels[row.kind as ScKind] }}</template>
        </el-table-column>
        <el-table-column prop="doc_date" label="日期" width="110" />
        <el-table-column label="內容" min-width="260">
          <template #default="{ row }">
            <template v-if="row.kind === 'receive'">
              收回 {{ row.qty }} {{ doc.unit_name }},加工費 {{ row.processing_fee
              }}<template v-if="Number(row.tax_amount) > 0">(稅 {{ row.tax_amount }})</template>
              <span v-for="u in row.output_lots" :key="u.lot_no" class="muted"
                >〔{{ u.lot_no }}(效期 {{ u.expiry_date ?? '—' }})〕</span
              >
            </template>
            <div v-for="l in row.lines" :key="l.line_no">
              {{ l.item_code }} × {{ l.qty }}
              <span v-if="l.lot_no" class="muted">〔{{ l.lot_no }}〕</span>
              <span v-for="u in l.lots" :key="u.lot_no" class="muted"
                >〔{{ u.lot_no }} × {{ u.qty }}〕</span
              >
            </div>
          </template>
        </el-table-column>
        <el-table-column label="狀態" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'posted' ? 'success' : 'info'">{{
              row.status === 'posted' ? '已過帳' : '已作廢'
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canPost" label="操作" width="90">
          <template #default="{ row }">
            <el-button v-if="row.status === 'posted'" link type="danger" @click="voidTxn(row)"
              >作廢</el-button
            >
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <SubcontractDialog
      v-if="doc"
      v-model="dlgVisible"
      :doc="doc"
      :mode="dlgMode"
      @done="applyDoc"
    />
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 16px;
}
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
}
.title h2 {
  margin: 0;
  font-size: 18px;
}
.actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.card-head {
  display: flex;
  align-items: center;
  gap: 12px;
}
.addline {
  margin-top: 8px;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.warn {
  color: var(--el-color-warning);
}
</style>
