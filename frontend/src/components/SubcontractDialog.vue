<script setup lang="ts">
// 委外加工的發料 / 退料 / 收回(D100);送出即過帳
//   發料:發料倉 → 加工廠的委外倉(材料所有權不變,只是位置在外)
//   退料:委外倉 → 回料倉
//   收回:扣委外倉的材料、成品入收回倉,加工費轉應付
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { productionApi, type ScKind, type SubcontractOrder } from '@/api/production'
import type { LotControl } from '@/api/masterdata'
import { useApiError } from '@/composables/useApiError'
import { useBinStore } from '@/composables/useBinStore'
import { scKindLabels } from '@/utils/subcontract'
import BinCell from '@/components/BinCell.vue'
import LotCell from '@/components/LotCell.vue'

const props = defineProps<{ doc: SubcontractOrder; mode: ScKind }>()
const emit = defineEmits<{ done: [SubcontractOrder] }>()
const visible = defineModel<boolean>({ default: false })

interface Row {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  lot_control: LotControl
  qty: string
  held: string
  lot_no: string
  bin_code: string
}

const { fieldErrors, handle, reset } = useApiError()
const binStore = useBinStore()
const saving = ref(false)
const remaining = ref('0')
const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const form = reactive({
  doc_date: today(),
  qty: '',
  processing_fee: '0',
  output_lot_no: '',
  output_expiry_date: null as string | null,
  output_bin_code: '',
  note: '',
  lines: [] as Row[],
})

const label = () => scKindLabels[props.mode]
// 材料出庫的倉庫:發料自發料倉,退料與收回自委外倉(委外倉不使用儲位)
const fromWh = () =>
  props.mode === 'issue' ? props.doc.issue_warehouse_id : props.doc.sc_warehouse_id
const fromBin = () => props.mode === 'issue' && binStore.usesBins(props.doc.issue_warehouse_id)
const returnBin = () => props.mode === 'return' && binStore.usesBins(props.doc.issue_warehouse_id)
const outputBin = () =>
  props.mode === 'receive' && binStore.usesBins(props.doc.receive_warehouse_id)
const lotControlled = (r: Row) => r.lot_control !== 'none'

function baseRows(): Row[] {
  return props.doc.lines
    .filter((l) => l.item_id !== null)
    .map((l) => ({
      item_id: l.item_id as number,
      item_code: l.item_code ?? '',
      item_name: l.item_name ?? '',
      unit_name: l.unit_name ?? '',
      lot_control: l.item_lot_control ?? 'none',
      qty: '',
      held: l.held_qty ?? '0',
      lot_no: '',
      bin_code: '',
    }))
}

/** 預設數量:發料 = 預計用量 − 已發;退料 = 目前還在加工廠的;收回由後端依本次數量計算 */
function defaults(): Row[] {
  const rows = baseRows()
  for (const r of rows) {
    const l = props.doc.lines.find((x) => x.item_id === r.item_id)!
    if (props.mode === 'issue') {
      const n = Number(l.qty) - Number(l.issued_qty ?? 0)
      r.qty = n > 0 ? String(Math.round(n * 10000) / 10000) : ''
    } else if (props.mode === 'return') {
      r.qty = Number(r.held) > 0 ? r.held : ''
    }
  }
  return props.mode === 'return' ? rows.filter((r) => Number(r.held) > 0) : rows
}

async function loadPreview() {
  try {
    const p = await productionApi.scReceivePreview(props.doc.id, form.qty || undefined)
    remaining.value = p.remaining
    if (!form.qty) form.qty = p.qty
    form.processing_fee = p.processing_fee
    form.lines = p.lines.map((l) => ({
      item_id: l.item_id,
      item_code: l.item_code,
      item_name: l.item_name,
      unit_name: l.unit_name,
      lot_control: props.doc.lines.find((x) => x.item_id === l.item_id)?.item_lot_control ?? 'none',
      qty: l.qty,
      held: l.held_qty,
      lot_no: '',
      bin_code: '',
    }))
  } catch (e) {
    handle(e)
  }
}

watch(visible, async (v) => {
  if (!v) return
  reset()
  await binStore.ensureWarehouses()
  Object.assign(form, {
    doc_date: today() < props.doc.doc_date ? props.doc.doc_date : today(),
    qty: '',
    output_lot_no: '',
    output_expiry_date: null,
    output_bin_code: '',
    note: '',
  })
  if (props.mode === 'receive') await loadPreview()
  else form.lines = defaults()
})

async function submit() {
  reset()
  saving.value = true
  try {
    const lines = form.lines
      .filter((r) => props.mode === 'receive' || Number(r.qty) > 0)
      .map((r) => ({
        item_id: r.item_id,
        qty: r.qty === '' ? '0' : r.qty,
        lot_no: lotControlled(r) ? r.lot_no : '',
        bin_code: fromBin() || returnBin() ? r.bin_code : '',
      }))
    const id = props.doc.id
    const next =
      props.mode === 'receive'
        ? await productionApi.scReceive(id, {
            doc_date: form.doc_date,
            qty: form.qty === '' ? '0' : form.qty,
            processing_fee: form.processing_fee === '' ? '0' : form.processing_fee,
            output_lot_no: props.doc.item_lot_control !== 'none' ? form.output_lot_no : '',
            output_bin_code: outputBin() ? form.output_bin_code : '',
            output_expiry_date:
              props.doc.item_lot_control === 'lot_expiry' ? form.output_expiry_date || null : null,
            note: form.note,
            lines: lines.map(({ item_id, qty, lot_no }) => ({ item_id, qty, lot_no })),
          })
        : await productionApi.scMaterial(id, props.mode, {
            doc_date: form.doc_date,
            note: form.note,
            lines,
          })
    ElMessage.success(`已${label()}`)
    visible.value = false
    emit('done', next)
  } catch (e) {
    handle(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <el-dialog v-model="visible" :title="`委外${label()}`" width="860px" top="6vh">
      <p class="hint">
        <template v-if="mode === 'issue'"
          >把材料從 {{ doc.issue_warehouse_name }} 轉到 {{ doc.supplier_name }} 的委外倉({{
            doc.sc_warehouse_name
          }});送出即過帳。</template
        >
        <template v-else-if="mode === 'return'"
          >把還在加工廠的材料退回 {{ doc.issue_warehouse_name }};送出即過帳。</template
        >
        <template v-else
          >預計 {{ doc.plan_qty }} {{ doc.unit_name }},已收回 {{ doc.received_qty }},尚未收回
          {{ remaining }}。送出後扣加工廠材料、成品入
          {{ doc.receive_warehouse_name }},加工費轉為應付。</template
        >
      </p>
      <el-form label-width="100px">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12">
            <el-form-item :label="`${label()}日期`" :error="fieldErrors.doc_date">
              <el-date-picker
                v-model="form.doc_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
                :clearable="false"
              />
            </el-form-item>
          </el-col>
          <template v-if="mode === 'receive'">
            <el-col :xs="24" :sm="12">
              <el-form-item label="本次收回量" :error="fieldErrors.qty">
                <el-input v-model="form.qty" @change="loadPreview">
                  <template #append>{{ doc.unit_name }}</template>
                </el-input>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12">
              <el-form-item label="本次加工費" :error="fieldErrors.processing_fee">
                <el-input v-model="form.processing_fee" />
              </el-form-item>
            </el-col>
            <el-col v-if="outputBin()" :xs="24" :sm="12">
              <el-form-item label="成品儲位" :error="fieldErrors.output_bin_code">
                <BinCell
                  v-model:code="form.output_bin_code"
                  :warehouse-id="doc.receive_warehouse_id"
                  mode="in"
                  :editable="true"
                />
              </el-form-item>
            </el-col>
            <template v-if="doc.item_lot_control !== 'none'">
              <el-col :xs="24" :sm="12">
                <el-form-item label="成品批號" :error="fieldErrors.output_lot_no">
                  <el-input v-model="form.output_lot_no" placeholder="這批成品入庫的批號" />
                </el-form-item>
              </el-col>
              <el-col v-if="doc.item_lot_control === 'lot_expiry'" :xs="24" :sm="12">
                <el-form-item label="成品效期">
                  <el-date-picker
                    v-model="form.output_expiry_date"
                    value-format="YYYY-MM-DD"
                    style="width: 100%"
                  />
                </el-form-item>
              </el-col>
            </template>
          </template>
          <el-col :span="24">
            <el-form-item label="備註">
              <el-input v-model="form.note" maxlength="255" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <div v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</div>
      <el-table :data="form.lines" border size="small">
        <el-table-column label="材料" min-width="200">
          <template #default="{ row, $index }">
            {{ row.item_code }} {{ row.item_name }}
            <div v-if="fieldErrors[`lines.${$index}`]" class="err">
              {{ fieldErrors[`lines.${$index}`] }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="mode === 'receive' ? '本次耗用量' : `${label()}量`" width="140">
          <template #default="{ row }">
            <el-input v-model="row.qty" size="small" />
            <span class="muted"> {{ row.unit_name }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="mode !== 'issue'" label="在加工廠" width="100" align="right">
          <template #default="{ row }">{{ row.held }}</template>
        </el-table-column>
        <el-table-column v-if="fromBin() || returnBin()" label="儲位" width="150">
          <template #default="{ row }">
            <BinCell
              v-model:code="row.bin_code"
              :warehouse-id="doc.issue_warehouse_id"
              :mode="mode === 'return' ? 'in' : 'out'"
              :editable="true"
            />
          </template>
        </el-table-column>
        <el-table-column label="批號" width="220">
          <template #default="{ row }">
            <LotCell
              v-model:lot-no="row.lot_no"
              :item-id="row.item_id"
              :control="row.lot_control === 'lot_expiry' ? 'lot' : row.lot_control"
              mode="out"
              :warehouse-id="fromWh()"
              :editable="true"
            />
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">{{ label() }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0 0 12px;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
</style>
