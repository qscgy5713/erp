<script setup lang="ts">
// 工單:成品、完工數量、領料明細(開單時依 BOM 展開,可調整實際用量);完工 = 一次領料並成品入庫
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { productionApi, type WorkOrder, type WorkOrderLine } from '@/api/production'
import type { DocAction, ItemOption } from '@/api/inventory'
import { masterdataApi, type LotControl, type Warehouse } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { actionLabels, allowedActions } from '@/utils/docstate'
import { formatDateTime } from '@/utils/format'
import type { ApprovalProgress as ApprovalProgressData } from '@/api/approval'
import ApprovalProgress from '@/components/ApprovalProgress.vue'
import DocStatusTag from '@/components/DocStatusTag.vue'
import ItemPicker from '@/components/ItemPicker.vue'
import LotCell from '@/components/LotCell.vue'
import BinCell from '@/components/BinCell.vue'
import { useBinStore } from '@/composables/useBinStore'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const doc = ref<WorkOrder | null>(null)
const isNew = computed(() => !doc.value)
const editable = computed(
  () => auth.can('production.order.write') && (isNew.value || doc.value?.status === 'draft'),
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

interface LineRow extends Omit<WorkOrderLine, 'item_id'> {
  item_id: number | null
}

const form = reactive({
  doc_date: today(),
  item_id: null as number | null,
  item_label: undefined as string | undefined,
  item_lot_control: 'none' as LotControl,
  plan_qty: '',
  warehouse_id: null as number | null,
  material_warehouse_id: null as number | null,
  processing_cost: '0',
  output_lot_no: '',
  output_bin_code: '',
  output_expiry_date: null as string | null,
  due_date: null as string | null,
  note: '',
  lines: [] as LineRow[],
})

function applyDoc(d: WorkOrder) {
  doc.value = d
  Object.assign(form, {
    doc_date: d.doc_date,
    item_id: d.item_id,
    item_label: `${d.item_code} ${d.item_name}`,
    item_lot_control: d.item_lot_control,
    plan_qty: d.plan_qty,
    warehouse_id: d.warehouse_id,
    material_warehouse_id: d.material_warehouse_id,
    processing_cost: d.processing_cost,
    output_lot_no: d.output_lot_no,
    output_bin_code: d.output_bin_code,
    output_expiry_date: d.output_expiry_date,
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
    applyDoc(await productionApi.order(id))
  } catch (e) {
    handle(e)
    router.replace({ name: 'work-orders' })
  } finally {
    loading.value = false
  }
}

// ---- 儲位:成品入庫倉啟用儲位時成品儲位必填;領料倉啟用儲位時領料可指定(留空自動分配) ----
const binStore = useBinStore()
const binTick = ref(0)
void binStore.ensureWarehouses().then(() => binTick.value++)
const usesBins = (id: number | null) => binTick.value >= 0 && !!id && binStore.usesBins(id)
const outputBin = computed(() => usesBins(form.warehouse_id))
const materialBin = computed(() => usesBins(form.material_warehouse_id))

// ---- 領料明細 ----

/** 使用者手動改過明細後,就不再因成品 / 數量變動而自動依 BOM 重算 */
const manualLines = ref(false)

async function explode(force = false) {
  if (!editable.value || !form.item_id || !Number(form.plan_qty)) return
  if (manualLines.value && !force) return
  try {
    const lines = await productionApi.explode(form.item_id, form.plan_qty, form.doc_date)
    form.lines = lines.map((l) => ({
      item_id: l.item_id,
      item_code: l.item_code,
      item_name: l.item_name,
      unit_name: l.unit_name,
      qty: l.qty,
      lot_no: '',
      note: '',
    }))
    manualLines.value = false
  } catch (e) {
    // 沒有 BOM 是可預期的狀況:顯示訊息,讓使用者自行輸入領料明細
    if (force) handle(e)
  }
}

function onPickOutput(item: ItemOption | null) {
  if (!item) return
  form.item_lot_control = item.lot_control
  form.output_lot_no = ''
  form.output_expiry_date = null
  if (!form.warehouse_id) {
    // 預設值留給使用者選;只在料品有預設倉庫時帶入(ItemOption 沒有,所以這裡不處理)
  }
  explode()
}

function onPickMaterial(row: LineRow, item: ItemOption | null) {
  manualLines.value = true
  if (!item) return
  row.item_code = item.code
  row.item_name = item.name
  row.unit_name = item.base_unit_name
  row.item_lot_control = item.lot_control
  row.lot_no = ''
}

function addLine() {
  manualLines.value = true
  form.lines.push({ item_id: null, qty: '', lot_no: '', note: '' })
}

const sentIndex = ref<number[]>([])
function lineError(i: number): string | undefined {
  const k = sentIndex.value.indexOf(i)
  return k < 0 ? undefined : fieldErrors.value[`lines.${k}`]
}

const lotControlled = (row: LineRow) => !!row.item_lot_control && row.item_lot_control !== 'none'

function payload() {
  sentIndex.value = form.lines.flatMap((l, i) => (l.item_id !== null ? [i] : []))
  return {
    doc_date: form.doc_date,
    item_id: form.item_id ?? 0,
    plan_qty: form.plan_qty === '' ? '0' : form.plan_qty,
    warehouse_id: form.warehouse_id ?? 0,
    material_warehouse_id: form.material_warehouse_id ?? 0,
    processing_cost: form.processing_cost === '' ? '0' : form.processing_cost,
    output_lot_no: form.item_lot_control !== 'none' ? form.output_lot_no : '',
    output_bin_code: outputBin.value ? form.output_bin_code : '',
    output_expiry_date:
      form.item_lot_control === 'lot_expiry' ? form.output_expiry_date || null : null,
    due_date: form.due_date || null,
    note: form.note,
    lines: form.lines
      .filter((l): l is LineRow & { item_id: number } => l.item_id !== null)
      .map((l) => ({
        item_id: l.item_id,
        qty: l.qty === '' ? '0' : l.qty,
        lot_no: lotControlled(l) ? (l.lot_no ?? '') : '',
        bin_code: materialBin.value ? (l.bin_code ?? '') : '',
        note: l.note,
      })),
    version: doc.value?.version,
  }
}

async function save(): Promise<boolean> {
  reset()
  saving.value = true
  try {
    const saved = doc.value
      ? await productionApi.updateOrder(doc.value.id, payload())
      : await productionApi.createOrder(payload())
    ElMessage.success('已儲存')
    if (isNew.value) {
      dirty.value = false
      router.replace({ name: 'work-order', params: { id: saved.id } })
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

// ---- 多層簽核(金額以加工費計) ----
const approvalInfo = ref<ApprovalProgressData | null>(null)
const progressRef = ref<InstanceType<typeof ApprovalProgress> | null>(null)
/** 套用多層簽核的待審工單:只有輪到的人才顯示「核准」 */
const approveBlocked = computed(
  () => doc.value?.status === 'pending' && !!approvalInfo.value && !approvalInfo.value.can_approve,
)

// ---- 狀態動作 ----

function canDo(action: DocAction): boolean {
  if (!doc.value) return false
  switch (action) {
    case 'submit':
      return auth.can('production.order.write')
    case 'approve':
      return auth.can('production.order.approve') && !approveBlocked.value
    case 'reject':
    case 'unapprove':
      return auth.can('production.order.approve')
    case 'post':
    case 'unpost':
      return auth.can('production.order.post')
    case 'void':
      return doc.value.status === 'draft'
        ? auth.can('production.order.write')
        : auth.can('production.order.approve')
    default:
      return false
  }
}

const actions = computed(() => (doc.value ? allowedActions(doc.value.status).filter(canDo) : []))
const acting = ref<DocAction | null>(null)

const labels: Partial<Record<DocAction, string>> = { post: '完工', unpost: '反完工' }
const label = (a: DocAction) => labels[a] ?? actionLabels[a]
const confirms: Partial<Record<DocAction, string>> = {
  post: '完工會依領料明細扣除材料庫存,並把成品入庫,確定完工?',
  unpost: '反完工會沖銷材料領用與成品入庫(成品若已被使用或出貨,庫存不足時會失敗),確定?',
  void: '作廢後無法復原,確定作廢?',
}

async function runAction(action: DocAction) {
  if (!doc.value) return
  const msg = confirms[action]
  if (msg) {
    try {
      await ElMessageBox.confirm(msg, label(action), {
        type: 'warning',
        confirmButtonText: label(action),
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  if (dirty.value && editable.value && !(await save())) return
  acting.value = action
  try {
    const next = await productionApi.action(doc.value.id, action, doc.value.version)
    applyDoc(next)
    ElMessage.success(
      action === 'approve' && next.status === 'pending'
        ? '已完成本層核准,等待下一層核准'
        : `已${label(action)}`,
    )
    progressRef.value?.reload()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

const short = (row: LineRow) =>
  doc.value?.status !== 'posted' &&
  row.on_hand !== undefined &&
  Number(row.qty) > Number(row.on_hand)

onMounted(async () => {
  try {
    warehouses.value = await masterdataApi.warehouses()
  } catch (e) {
    handle(e)
  }
  await load()
  setTimeout(() => (dirty.value = false))
})
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: 'work-orders' })">← 返回列表</el-button>
        <h2>工單 {{ doc?.doc_no ?? '(新單據)' }}</h2>
        <DocStatusTag v-if="doc" :status="doc.status" />
        <el-tag v-if="dirty && editable && doc" type="warning" effect="plain">未儲存</el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">儲存</el-button>
        <el-button
          v-for="a in actions"
          :key="a"
          :type="
            a === 'post' || a === 'approve'
              ? 'success'
              : a === 'void' || a === 'unpost'
                ? 'danger'
                : 'default'
          "
          :loading="acting === a"
          @click="runAction(a)"
        >
          {{ label(a) }}
        </el-button>
      </div>
    </div>

    <ApprovalProgress
      ref="progressRef"
      doc-type="work_order"
      :doc-id="doc?.id"
      :status="doc?.status"
      :version="doc?.version"
      @loaded="approvalInfo = $event"
    />

    <el-card shadow="never" class="mb">
      <el-form label-width="100px" :disabled="!editable">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-form-item label="成品" :error="fieldErrors.item_id">
              <ItemPicker
                v-model="form.item_id"
                :label="form.item_label"
                item-type="goods"
                :disabled="!editable"
                @select="onPickOutput"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="完工數量" :error="fieldErrors.plan_qty">
              <el-input v-model="form.plan_qty" @change="explode()" />
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
            <el-form-item label="成品入庫倉" :error="fieldErrors.warehouse_id">
              <el-select v-model="form.warehouse_id" style="width: 100%">
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
            <el-form-item label="領料倉" :error="fieldErrors.material_warehouse_id">
              <el-select v-model="form.material_warehouse_id" style="width: 100%">
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
            <el-form-item label="加工費" :error="fieldErrors.processing_cost">
              <el-input v-model="form.processing_cost" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="預定完工日">
              <el-date-picker
                v-model="form.due_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col v-if="outputBin" :xs="24" :sm="12" :md="6">
            <el-form-item label="成品儲位" :error="fieldErrors.output_bin_code">
              <BinCell
                v-model:code="form.output_bin_code"
                :warehouse-id="form.warehouse_id"
                mode="in"
                :editable="editable"
              />
            </el-form-item>
          </el-col>
          <template v-if="form.item_lot_control !== 'none'">
            <el-col :xs="24" :sm="12" :md="6">
              <el-form-item label="成品批號" :error="fieldErrors.output_lot_no">
                <el-input v-model="form.output_lot_no" placeholder="成品入庫的批號" />
              </el-form-item>
            </el-col>
            <el-col v-if="form.item_lot_control === 'lot_expiry'" :xs="24" :sm="12" :md="6">
              <el-form-item label="成品效期">
                <el-date-picker
                  v-model="form.output_expiry_date"
                  value-format="YYYY-MM-DD"
                  style="width: 100%"
                />
              </el-form-item>
            </el-col>
          </template>
          <el-col :span="24">
            <el-form-item label="備註"
              ><el-input v-model="form.note" maxlength="2000"
            /></el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <div v-if="doc?.status === 'posted' && doc.output_lots.length" class="muted">
        成品入庫批號:
        <span v-for="u in doc.output_lots" :key="u.lot_no"
          >{{ u.lot_no }} × {{ u.qty }}(效期 {{ u.expiry_date ?? '—' }})</span
        >
      </div>
    </el-card>

    <el-card shadow="never" class="mb">
      <template #header>
        <div class="card-head">
          領料明細({{ form.lines.filter((l) => l.item_id).length }} 筆)
          <el-button v-if="editable" size="small" @click="explode(true)">依 BOM 重新計算</el-button>
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
        <el-table-column label="用量" width="150">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.qty" size="small" @input="manualLines = true" />
            <span v-else>{{ row.qty }}</span>
            <span class="muted"> {{ row.unit_name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="領料倉現有量" width="130" align="right">
          <template #default="{ row }">
            <span :class="{ neg: short(row) }">{{ row.on_hand ?? '' }}</span>
            <el-tag v-if="short(row)" size="small" type="danger">不足</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="materialBin" label="儲位" width="150">
          <template #default="{ row }">
            <BinCell
              v-model:code="row.bin_code"
              :warehouse-id="form.material_warehouse_id"
              mode="out"
              :editable="editable"
            />
          </template>
        </el-table-column>
        <el-table-column label="批號" width="230">
          <template #default="{ row }">
            <LotCell
              v-model:lot-no="row.lot_no"
              :item-id="row.item_id"
              :control="row.item_lot_control === 'lot_expiry' ? 'lot' : row.item_lot_control"
              mode="out"
              :warehouse-id="form.material_warehouse_id"
              :editable="editable"
              :lots="row.lots"
            />
          </template>
        </el-table-column>
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

    <el-descriptions v-if="doc" :column="3" size="small" border>
      <el-descriptions-item label="建立">{{ doc.created_by_name }}</el-descriptions-item>
      <el-descriptions-item label="送審"
        >{{ doc.submitted_by_name }} {{ formatDateTime(doc.submitted_at) }}</el-descriptions-item
      >
      <el-descriptions-item label="核准"
        >{{ doc.approved_by_name }} {{ formatDateTime(doc.approved_at) }}</el-descriptions-item
      >
      <el-descriptions-item label="完工"
        >{{ doc.posted_by_name }} {{ formatDateTime(doc.posted_at) }}</el-descriptions-item
      >
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
}
.title h2 {
  margin: 0;
  font-size: 18px;
}
.mb {
  margin-bottom: 12px;
}
.card-head {
  display: flex;
  gap: 12px;
  align-items: center;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.neg {
  color: var(--el-color-danger);
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.addline {
  margin-top: 8px;
}
</style>
