<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import {
  inventoryApi,
  type DocAction,
  type ItemOption,
  type StockDocType,
  type StockDocument,
  type StockLine,
} from '@/api/inventory'
import { masterdataApi, type ItemCategory, type Warehouse } from '@/api/masterdata'
import type { ItemUnit } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { actionLabels, allowedActions, confirmActions } from '@/utils/docstate'
import { formatDateTime } from '@/utils/format'
import { buildTree } from '@/utils/tree'
import DocStatusTag from '@/components/DocStatusTag.vue'
import ItemPicker from '@/components/ItemPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const typeLabels: Record<StockDocType, string> = {
  adjustment: '庫存調整單',
  transfer: '調撥單',
  count: '盤點單',
}

const doc = ref<StockDocument | null>(null)
const isNew = computed(() => !doc.value)
const docType = computed<StockDocType>(
  () => doc.value?.doc_type ?? (route.query.type as StockDocType) ?? 'adjustment',
)
const editable = computed(
  () => auth.can('inventory.stock.write') && (isNew.value || doc.value?.status === 'draft'),
)
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)

const warehouses = ref<Warehouse[]>([])
const categories = ref<ItemCategory[]>([])
const activeWarehouses = computed(() => warehouses.value.filter((w) => w.is_active))

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// 新增列尚未選料品與單位時為 null(用 0 會被下拉選單直接顯示成「0」)
interface LineRow extends Omit<StockLine, 'item_id' | 'unit_id'> {
  item_id: number | null
  unit_id: number | null
  /** 前端用:此料品可選的單位 */
  unitOptions: { id: number; name: string; factor: string }[]
}

const formRef = ref<FormInstance>()
const form = reactive({
  doc_date: today(),
  warehouse_id: null as number | null,
  to_warehouse_id: null as number | null,
  category_id: null as number | null,
  note: '',
  lines: [] as LineRow[],
})

function unitOptionsOf(item: ItemOption): LineRow['unitOptions'] {
  return [
    { id: item.base_unit_id, name: item.base_unit_name, factor: '1' },
    ...item.units.map((u: ItemUnit) => ({
      id: u.unit_id,
      name: u.unit_name ?? '',
      factor: u.factor,
    })),
  ]
}

function applyDoc(d: StockDocument) {
  doc.value = d
  Object.assign(form, {
    doc_date: d.doc_date,
    warehouse_id: d.warehouse_id,
    to_warehouse_id: d.to_warehouse_id,
    category_id: d.category_id,
    note: d.note,
    lines: d.lines.map((l) => ({
      ...l,
      unitOptions: [{ id: l.unit_id, name: l.unit_name ?? '', factor: l.factor ?? '1' }],
    })),
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
    applyDoc(await inventoryApi.document(id))
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

// 既有明細只有已選單位;展開下拉時才去載入該料品的全部單位
async function loadUnits(row: LineRow) {
  if (row.unitOptions.length > 1 || !row.item_code) return
  try {
    const found = (await inventoryApi.itemOptions(row.item_code)).find((o) => o.id === row.item_id)
    if (found) row.unitOptions = unitOptionsOf(found)
  } catch (e) {
    handle(e)
  }
}

function onPickItem(row: LineRow, item: ItemOption | null) {
  if (!item) return
  row.item_code = item.code
  row.item_name = item.name
  row.base_unit_name = item.base_unit_name
  row.unitOptions = unitOptionsOf(item)
  row.unit_id = item.base_unit_id
  row.factor = '1'
  row.system_qty = null // 盤點新增料品:帳面數由後端依目前現有量帶入
}

function addLine() {
  form.lines.push({ item_id: null, unit_id: null, qty: null, note: '', unitOptions: [] })
}

/** 換算後的基本單位數量(僅顯示用,以後端為準) */
function baseQtyOf(row: LineRow): string {
  const f = row.unitOptions.find((u) => u.id === row.unit_id)?.factor ?? row.factor ?? '1'
  if (row.qty === null || row.qty === '' || Number.isNaN(Number(row.qty))) return ''
  return String(Math.round(Number(row.qty) * Number(f) * 10000) / 10000)
}

function diffOf(row: LineRow): string {
  if (row.qty === null || row.qty === '' || row.system_qty == null) return ''
  return String(Math.round((Number(row.qty) - Number(row.system_qty)) * 10000) / 10000)
}

// 後端錯誤以「送出的明細序號」標示;空白列送出前會被略過,需對應回畫面上的列
const sentIndex = ref<number[]>([])

function lineError(uiIndex: number): string | undefined {
  const i = sentIndex.value.indexOf(uiIndex)
  return i < 0 ? undefined : fieldErrors.value[`lines.${i}`]
}

function payload() {
  sentIndex.value = form.lines.flatMap((l, i) => (l.item_id !== null ? [i] : []))
  return {
    doc_type: docType.value,
    doc_date: form.doc_date,
    warehouse_id: form.warehouse_id!,
    to_warehouse_id: docType.value === 'transfer' ? form.to_warehouse_id : null,
    category_id: docType.value === 'count' ? form.category_id : null,
    note: form.note,
    // 未選料品的空白列略過;未選單位送 0,由後端回報「沒有此單位」
    lines: form.lines
      .filter((l): l is LineRow & { item_id: number } => l.item_id !== null)
      .map((l) => ({
        item_id: l.item_id,
        unit_id: l.unit_id ?? 0,
        qty: l.qty === '' ? null : l.qty,
        note: l.note,
      })),
  }
}

async function save(): Promise<boolean> {
  if (!form.warehouse_id) {
    fieldErrors.value = { warehouse_id: '請選擇倉庫' }
    return false
  }
  saving.value = true
  try {
    const saved = doc.value
      ? await inventoryApi.update(doc.value.id, { ...payload(), version: doc.value.version })
      : await inventoryApi.create(payload())
    applyDoc(saved)
    ElMessage.success('已儲存')
    if (route.name !== 'inventory-document') {
      router.replace({ name: 'inventory-document', params: { id: saved.id } })
    }
    return true
  } catch (e) {
    handle(e)
    return false
  } finally {
    saving.value = false
  }
}

// ---- 狀態動作 ----

function canDo(action: DocAction): boolean {
  if (!doc.value) return false
  switch (action) {
    case 'submit':
      return auth.can('inventory.stock.write')
    case 'approve':
    case 'reject':
    case 'unapprove':
      return auth.can('inventory.stock.approve')
    case 'post':
    case 'unpost':
      return auth.can('inventory.stock.post')
    case 'void':
      return doc.value.status === 'draft'
        ? auth.can('inventory.stock.write')
        : auth.can('inventory.stock.approve')
  }
}

const actions = computed(() => (doc.value ? allowedActions(doc.value.status).filter(canDo) : []))
const acting = ref<DocAction | null>(null)

async function runAction(action: DocAction) {
  if (!doc.value) return
  if (confirmActions[action]) {
    try {
      await ElMessageBox.confirm(confirmActions[action]!, actionLabels[action], {
        type: 'warning',
        confirmButtonText: actionLabels[action],
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  // 草稿有未儲存的修改時先存檔
  if (dirty.value && editable.value && !(await save())) return
  acting.value = action
  try {
    applyDoc(await inventoryApi.action(doc.value.id, action, doc.value.version))
    ElMessage.success(`已${actionLabels[action]}`)
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

onMounted(async () => {
  try {
    ;[warehouses.value, categories.value] = await Promise.all([
      masterdataApi.warehouses(),
      masterdataApi.categories(),
    ])
  } catch (e) {
    handle(e)
  }
  await load()
  if (isNew.value && docType.value !== 'count') addLine()
  setTimeout(() => (dirty.value = false))
})
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: 'inventory-documents' })">← 返回列表</el-button>
        <h2>{{ typeLabels[docType] }} {{ doc?.doc_no ?? '(新單據)' }}</h2>
        <DocStatusTag v-if="doc" :status="doc.status" />
        <el-tag v-if="dirty && editable && doc" type="warning" effect="plain">未儲存</el-tag>
      </div>
      <div class="actions">
        <el-button
          v-if="editable && !(isNew && docType === 'count')"
          type="primary"
          :loading="saving"
          @click="save"
        >
          儲存
        </el-button>
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
          {{ actionLabels[a] }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="docType === 'count' && doc && ['draft', 'pending', 'approved'].includes(doc.status)"
      type="warning"
      :closable="false"
      show-icon
      title="盤點進行中:此倉庫在盤點單過帳或作廢前,其他單據無法異動庫存。"
      class="mb"
    />

    <el-card shadow="never" class="mb">
      <el-form ref="formRef" :model="form" label-width="80px" :disabled="!editable">
        <el-row :gutter="16">
          <el-col :span="6">
            <el-form-item label="日期" :error="fieldErrors.doc_date">
              <el-date-picker
                v-model="form.doc_date"
                value-format="YYYY-MM-DD"
                :clearable="false"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :span="6">
            <el-form-item
              :label="docType === 'transfer' ? '調出倉' : '倉庫'"
              :error="fieldErrors.warehouse_id"
            >
              <el-select v-model="form.warehouse_id" :disabled="!isNew" style="width: 100%">
                <el-option
                  v-for="w in activeWarehouses"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col v-if="docType === 'transfer'" :span="6">
            <el-form-item label="調入倉" :error="fieldErrors.to_warehouse_id">
              <el-select v-model="form.to_warehouse_id" style="width: 100%">
                <el-option
                  v-for="w in activeWarehouses.filter((x) => x.id !== form.warehouse_id)"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col v-if="docType === 'count'" :span="6">
            <el-form-item label="盤點範圍">
              <el-tree-select
                v-model="form.category_id"
                :data="buildTree(categories)"
                :props="{ label: 'name', children: 'children' }"
                node-key="id"
                check-strictly
                clearable
                :disabled="!isNew"
                placeholder="全部分類"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="備註"
          ><el-input v-model="form.note" type="textarea" :rows="1" maxlength="2000"
        /></el-form-item>
      </el-form>
      <el-alert
        v-if="docType === 'count' && isNew"
        type="info"
        :closable="false"
        show-icon
        title="建立後系統會依倉庫目前的現有量產生盤點明細(帳面數快照),並凍結該倉庫的庫存異動。"
      />
    </el-card>

    <el-card v-if="!(docType === 'count' && isNew)" shadow="never">
      <template #header>
        明細({{ form.lines.filter((l) => l.item_id).length }} 筆)
        <span v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</span>
      </template>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column label="料品" min-width="240">
          <template #default="{ row, $index }">
            <ItemPicker
              v-if="editable && !(docType === 'count' && row.system_qty != null && row.id)"
              v-model="row.item_id"
              :label="row.item_code ? `${row.item_code} ${row.item_name}` : undefined"
              item-type="goods"
              @select="(it) => onPickItem(row, it)"
            />
            <span v-else>{{ row.item_code }} {{ row.item_name }}</span>
            <div v-if="lineError($index)" class="err">
              {{ lineError($index) }}
            </div>
          </template>
        </el-table-column>

        <template v-if="docType === 'count'">
          <el-table-column label="帳面數" width="110" align="right">
            <template #default="{ row }">{{ row.system_qty ?? '(過帳時帶入)' }}</template>
          </el-table-column>
          <el-table-column label="實盤數" width="150">
            <template #default="{ row }">
              <el-input v-if="editable" v-model="row.qty" size="small" placeholder="未盤" />
              <span v-else>{{ row.qty }}</span>
            </template>
          </el-table-column>
          <el-table-column label="差異" width="100" align="right">
            <template #default="{ row }">
              <span :class="{ neg: Number(diffOf(row)) < 0, pos: Number(diffOf(row)) > 0 }">{{
                diffOf(row)
              }}</span>
            </template>
          </el-table-column>
          <el-table-column label="單位" width="80">
            <template #default="{ row }">{{ row.base_unit_name }}</template>
          </el-table-column>
        </template>

        <template v-else>
          <el-table-column label="單位" width="120">
            <template #default="{ row }">
              <el-select
                v-if="editable"
                v-model="row.unit_id"
                size="small"
                :disabled="!row.item_id"
                @visible-change="(v: boolean) => v && loadUnits(row)"
              >
                <el-option v-for="u in row.unitOptions" :key="u.id" :label="u.name" :value="u.id" />
              </el-select>
              <span v-else>{{ row.unit_name }}</span>
            </template>
          </el-table-column>
          <el-table-column
            :label="docType === 'adjustment' ? '數量(負數為減少)' : '數量'"
            width="160"
          >
            <template #default="{ row }">
              <el-input v-if="editable" v-model="row.qty" size="small" />
              <span v-else>{{ row.qty }}</span>
            </template>
          </el-table-column>
          <el-table-column label="基本單位數量" width="130" align="right">
            <template #default="{ row }">
              {{ editable ? baseQtyOf(row) : row.base_qty }} {{ row.base_unit_name }}
            </template>
          </el-table-column>
        </template>

        <el-table-column label="備註" min-width="140">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.note" size="small" maxlength="255" />
            <span v-else>{{ row.note }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="editable" width="60">
          <template #default="{ row, $index }">
            <!-- 盤點明細只能新增,已存檔的列不可刪除(避免藏起盤虧) -->
            <el-button
              v-if="!(docType === 'count' && row.id)"
              link
              type="danger"
              @click="form.lines.splice($index, 1)"
            >
              刪除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-button v-if="editable" class="mt" @click="addLine">新增明細</el-button>
    </el-card>

    <el-card v-if="isNew && docType === 'count'" shadow="never">
      <el-button type="primary" :loading="saving" @click="save">建立盤點單</el-button>
    </el-card>

    <el-descriptions v-if="doc" :column="4" border size="small" class="mt">
      <el-descriptions-item label="建立">{{ doc.created_by_name }}</el-descriptions-item>
      <el-descriptions-item label="送審">
        {{ doc.submitted_by_name }} {{ formatDateTime(doc.submitted_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="核准">
        {{ doc.approved_by_name }} {{ formatDateTime(doc.approved_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="過帳"
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
.mt {
  margin-top: 12px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
  margin-left: 8px;
}
.neg {
  color: var(--el-color-danger);
}
.pos {
  color: var(--el-color-success);
}
</style>
