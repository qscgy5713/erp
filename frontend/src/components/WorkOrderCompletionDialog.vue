<script setup lang="ts">
// 工單完工回報(分批完工,D94):填完工日期與本次數量,領料預設依工單比例、可逐筆改用量 / 批號 / 儲位;送出即過帳
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { productionApi, type WorkOrder, type WorkOrderLine } from '@/api/production'
import { useApiError } from '@/composables/useApiError'
import { useBinStore } from '@/composables/useBinStore'
import BinCell from '@/components/BinCell.vue'
import LotCell from '@/components/LotCell.vue'

const props = defineProps<{ doc: WorkOrder }>()
const emit = defineEmits<{ done: [WorkOrder] }>()
const visible = defineModel<boolean>({ default: false })

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
  processing_cost: '0',
  output_lot_no: '',
  output_expiry_date: null as string | null,
  output_bin_code: '',
  note: '',
  lines: [] as WorkOrderLine[],
})

const outputBin = () => binStore.usesBins(props.doc.warehouse_id)
const materialBin = () => binStore.usesBins(props.doc.material_warehouse_id)

/** 依本次數量向後端取預設領料與加工費(數量改了就重算,自訂過的領料會被覆蓋) */
async function loadPreview() {
  try {
    const p = await productionApi.completionPreview(props.doc.id, form.qty || undefined)
    remaining.value = p.remaining
    if (!form.qty) form.qty = p.qty
    form.processing_cost = p.processing_cost
    form.lines = p.lines.map((l) => ({ ...l }))
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
  await loadPreview()
})

async function submit() {
  reset()
  saving.value = true
  try {
    const next = await productionApi.complete(props.doc.id, {
      doc_date: form.doc_date,
      qty: form.qty === '' ? '0' : form.qty,
      processing_cost: form.processing_cost === '' ? '0' : form.processing_cost,
      output_lot_no: props.doc.item_lot_control !== 'none' ? form.output_lot_no : '',
      output_bin_code: outputBin() ? form.output_bin_code : '',
      output_expiry_date:
        props.doc.item_lot_control === 'lot_expiry' ? form.output_expiry_date || null : null,
      note: form.note,
      lines: form.lines.map((l) => ({
        item_id: l.item_id,
        qty: l.qty === '' ? '0' : l.qty,
        lot_no: l.item_lot_control && l.item_lot_control !== 'none' ? (l.lot_no ?? '') : '',
        bin_code: materialBin() ? (l.bin_code ?? '') : '',
      })),
    })
    ElMessage.success('已完工回報')
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
  <el-dialog v-model="visible" title="完工回報" width="820px" top="6vh">
    <p class="hint">
      工單 {{ doc.doc_no }}:計畫 {{ doc.plan_qty }} {{ doc.unit_name }},已完工
      {{ doc.completed_qty }},尚未完工
      {{ remaining }}。送出後立即領料並把成品入庫(已領到在製品倉的材料優先使用,不足才從領料倉扣)。
    </p>
    <el-form label-width="100px">
      <el-row :gutter="16">
        <el-col :xs="24" :sm="12">
          <el-form-item label="完工日期" :error="fieldErrors.doc_date">
            <el-date-picker
              v-model="form.doc_date"
              value-format="YYYY-MM-DD"
              style="width: 100%"
              :clearable="false"
            />
          </el-form-item>
        </el-col>
        <el-col :xs="24" :sm="12">
          <el-form-item label="本次完工量" :error="fieldErrors.qty || fieldErrors.plan_qty">
            <el-input v-model="form.qty" @change="loadPreview">
              <template #append>{{ doc.unit_name }}</template>
            </el-input>
          </el-form-item>
        </el-col>
        <el-col :xs="24" :sm="12">
          <el-form-item label="本次加工費" :error="fieldErrors.processing_cost">
            <el-input v-model="form.processing_cost" />
          </el-form-item>
        </el-col>
        <el-col v-if="outputBin()" :xs="24" :sm="12">
          <el-form-item label="成品儲位" :error="fieldErrors.output_bin_code">
            <BinCell
              v-model:code="form.output_bin_code"
              :warehouse-id="doc.warehouse_id"
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
      <el-table-column label="本次領料量" width="140">
        <template #default="{ row }">
          <el-input v-model="row.qty" size="small" />
          <span class="muted"> {{ row.unit_name }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="doc.wip.held.length" label="在製品可用" width="100" align="right">
        <template #default="{ row }">{{
          doc.wip.held.find((h) => h.item_id === row.item_id)?.qty ?? '0'
        }}</template>
      </el-table-column>
      <el-table-column v-if="materialBin()" label="儲位" width="150">
        <template #default="{ row }">
          <BinCell
            v-model:code="row.bin_code"
            :warehouse-id="doc.material_warehouse_id"
            mode="out"
            :editable="true"
          />
        </template>
      </el-table-column>
      <el-table-column label="批號" width="220">
        <template #default="{ row }">
          <LotCell
            v-model:lot-no="row.lot_no"
            :item-id="row.item_id"
            :control="row.item_lot_control === 'lot_expiry' ? 'lot' : row.item_lot_control"
            mode="out"
            :warehouse-id="doc.material_warehouse_id"
            :editable="true"
          />
        </template>
      </el-table-column>
    </el-table>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">完工回報</el-button>
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
