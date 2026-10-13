<script setup lang="ts">
// 工單領料 / 退料(在製品,D101):完工前把材料從領料倉轉到在製品倉,或把用不到的退回;送出即過帳
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { productionApi, type WorkOrder } from '@/api/production'
import type { LotControl } from '@/api/masterdata'
import { useApiError } from '@/composables/useApiError'
import { useBinStore } from '@/composables/useBinStore'
import BinCell from '@/components/BinCell.vue'
import LotCell from '@/components/LotCell.vue'

const props = defineProps<{ doc: WorkOrder; mode: 'issue' | 'return' }>()
const emit = defineEmits<{ done: [WorkOrder] }>()
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
const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
const form = reactive({ doc_date: today(), note: '', lines: [] as Row[] })

const label = () => (props.mode === 'issue' ? '領料' : '退料')
const heldOf = (id: number) => props.doc.wip.held.find((h) => h.item_id === id)?.qty ?? '0'
// 領料自領料倉出;退料自在製品倉出(不使用儲位),回料倉的入庫儲位由使用者指定
const fromBin = () => props.mode === 'issue' && binStore.usesBins(props.doc.material_warehouse_id)
const returnBin = () =>
  props.mode === 'return' && binStore.usesBins(props.doc.material_warehouse_id)
const round4 = (n: number) => String(Math.round(n * 10000) / 10000)

function defaults(): Row[] {
  const remainingRatio =
    (Number(props.doc.plan_qty) - Number(props.doc.completed_qty)) / Number(props.doc.plan_qty)
  if (props.mode === 'issue') {
    return props.doc.lines.map((l) => {
      const need = Number(l.qty) * remainingRatio - Number(heldOf(l.item_id))
      return {
        item_id: l.item_id,
        item_code: l.item_code ?? '',
        item_name: l.item_name ?? '',
        unit_name: l.unit_name ?? '',
        lot_control: l.item_lot_control ?? 'none',
        qty: need > 0 ? round4(need) : '',
        held: heldOf(l.item_id),
        lot_no: '',
        bin_code: '',
      }
    })
  }
  return props.doc.wip.held.map((h) => ({
    item_id: h.item_id,
    item_code: h.item_code,
    item_name: h.item_name,
    unit_name: h.unit_name,
    lot_control: props.doc.lines.find((l) => l.item_id === h.item_id)?.item_lot_control ?? 'none',
    qty: h.qty,
    held: h.qty,
    lot_no: '',
    bin_code: '',
  }))
}

watch(visible, async (v) => {
  if (!v) return
  reset()
  await binStore.ensureWarehouses()
  Object.assign(form, {
    doc_date: today() < props.doc.doc_date ? props.doc.doc_date : today(),
    note: '',
  })
  form.lines = defaults()
})

async function submit() {
  reset()
  saving.value = true
  try {
    const next = await productionApi.wipTxn(props.doc.id, props.mode, {
      doc_date: form.doc_date,
      note: form.note,
      lines: form.lines
        .filter((r) => Number(r.qty) > 0)
        .map((r) => ({
          item_id: r.item_id,
          qty: r.qty,
          lot_no: r.lot_control !== 'none' ? r.lot_no : '',
          bin_code: fromBin() || returnBin() ? r.bin_code : '',
        })),
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
  <el-dialog v-model="visible" :title="`工單${label()}(在製品)`" width="820px" top="6vh">
    <p class="hint">
      <template v-if="mode === 'issue'"
        >把材料從
        {{ doc.material_warehouse_name }}
        轉到在製品倉;之後完工回報會優先扣在製品。送出即過帳。</template
      >
      <template v-else
        >把用不到的在製品材料退回 {{ doc.material_warehouse_name }};送出即過帳。</template
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
        <el-col :xs="24" :sm="12">
          <el-form-item label="備註"><el-input v-model="form.note" maxlength="255" /></el-form-item>
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
      <el-table-column :label="`${label()}量`" width="140">
        <template #default="{ row }">
          <el-input v-model="row.qty" size="small" />
          <span class="muted"> {{ row.unit_name }}</span>
        </template>
      </el-table-column>
      <el-table-column label="在製品" width="100" align="right">
        <template #default="{ row }">{{ row.held }}</template>
      </el-table-column>
      <el-table-column v-if="fromBin() || returnBin()" label="儲位" width="150">
        <template #default="{ row }">
          <BinCell
            v-model:code="row.bin_code"
            :warehouse-id="doc.material_warehouse_id"
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
            :warehouse-id="mode === 'issue' ? doc.material_warehouse_id : null"
            :editable="mode === 'issue'"
          />
        </template>
      </el-table-column>
    </el-table>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">{{ label() }}</el-button>
    </template>
  </el-dialog>
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
