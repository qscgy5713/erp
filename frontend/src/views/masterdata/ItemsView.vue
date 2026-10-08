<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  lotControlLabels,
  masterdataApi,
  type Item,
  type ItemCategory,
  type TaxType,
  type Unit,
  type Warehouse,
} from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { buildTree } from '@/utils/tree'
import { decimalRule, required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('masterdata.item.write'))

const query = reactive({
  keyword: '',
  category_id: null as number | null,
  item_type: '',
  is_active: null as boolean | null,
  page: 1,
  size: 20,
})
const rows = ref<Item[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

const units = ref<Unit[]>([])
const categories = ref<ItemCategory[]>([])
const taxTypes = ref<TaxType[]>([])
const warehouses = ref<Warehouse[]>([])
const categoryTree = computed(() => buildTree(categories.value))
const activeUnits = computed(() => units.value.filter((u) => u.is_active))
const unitName = (id: number) => units.value.find((u) => u.id === id)?.name ?? ''

const typeLabels = { goods: '商品', service: '服務/費用' }

async function load() {
  loading.value = true
  try {
    const res = await masterdataApi.items({ ...query, keyword: query.keyword.trim() })
    rows.value = res.items
    meta.value = res.meta
  } catch (e) {
    dlg.handleError(e)
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

interface UnitRow {
  unit_id: number | null
  factor: string
  barcode: string
}

interface ItemForm {
  code: string
  name: string
  spec: string
  category_id: number | null
  item_type: Item['item_type']
  lot_control: Item['lot_control']
  base_unit_id: number | null
  barcode: string
  tax_type_id: number | null
  default_warehouse_id: number | null
  safety_stock: string
  list_price: string
  note: string
  units: UnitRow[]
  is_active: boolean
}

// 空字串轉 null,未選的換算單位列略過
function toPayload(f: ItemForm) {
  return {
    ...f,
    base_unit_id: f.base_unit_id!,
    barcode: f.barcode.trim() || null,
    units: f.units
      .filter((u) => u.unit_id !== null)
      .map((u) => ({
        unit_id: u.unit_id!,
        factor: u.factor.trim(),
        barcode: u.barcode.trim() || null,
      })),
  }
}

const dlg = useFormDialog<ItemForm, Item>({
  defaults: () => ({
    code: '',
    name: '',
    spec: '',
    category_id: null,
    item_type: 'goods',
    lot_control: 'none',
    base_unit_id: null,
    barcode: '',
    tax_type_id: taxTypes.value.find((t) => t.code === 'TX5')?.id ?? null,
    default_warehouse_id: null,
    safety_stock: '0',
    list_price: '0',
    note: '',
    units: [],
    is_active: true,
  }),
  fromRow: (r: Item) => ({
    code: r.code,
    name: r.name,
    spec: r.spec,
    category_id: r.category_id,
    item_type: r.item_type,
    lot_control: r.lot_control ?? 'none',
    base_unit_id: r.base_unit_id,
    barcode: r.barcode ?? '',
    tax_type_id: r.tax_type_id,
    default_warehouse_id: r.default_warehouse_id,
    safety_stock: r.safety_stock,
    list_price: r.list_price,
    note: r.note,
    units: r.units.map((u) => ({ unit_id: u.unit_id, factor: u.factor, barcode: u.barcode ?? '' })),
    is_active: r.is_active,
  }),
  create: (f) => masterdataApi.item.create(toPayload(f)),
  update: (id, f) => masterdataApi.item.update(id, { ...toPayload(f), version: f.version }),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg
const dialogTab = ref('basic')

// 錯誤只出現在單位換算時,自動切到該分頁,避免使用者看不到錯誤
watch(fieldErrors, (errs) => {
  const keys = Object.keys(errs)
  if (keys.length && keys.every((k) => k.startsWith('units'))) dialogTab.value = 'units'
})

function openCreate() {
  dialogTab.value = 'basic'
  dlg.openCreate()
}

async function openEdit(row: Item) {
  dialogTab.value = 'basic'
  dlg.openEdit(row)
}

function addUnitRow() {
  form.units.push({ unit_id: null, factor: '', barcode: '' })
}

onMounted(async () => {
  load()
  try {
    ;[units.value, categories.value, taxTypes.value, warehouses.value] = await Promise.all([
      masterdataApi.units(),
      masterdataApi.categories(),
      masterdataApi.taxTypes(),
      masterdataApi.warehouses(),
    ])
  } catch (e) {
    dlg.handleError(e)
  }
})
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-input
        v-model="query.keyword"
        placeholder="料號、品名、規格或條碼"
        clearable
        style="width: 220px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-tree-select
        v-model="query.category_id"
        :data="categoryTree"
        :props="{ label: 'name', children: 'children' }"
        node-key="id"
        check-strictly
        clearable
        placeholder="分類(含下層)"
        style="width: 180px"
        @change="search"
      />
      <el-select
        v-model="query.item_type"
        clearable
        placeholder="類型"
        style="width: 120px"
        @change="search"
      >
        <el-option v-for="(label, v) in typeLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-select
        v-model="query.is_active"
        clearable
        placeholder="狀態"
        style="width: 100px"
        @change="search"
      >
        <el-option label="啟用" :value="true" />
        <el-option label="停用" :value="false" />
      </el-select>
      <el-button @click="search">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="openCreate">新增料品</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="code" label="料號" width="140" />
      <el-table-column prop="name" label="品名" min-width="160" />
      <el-table-column prop="spec" label="規格" min-width="140" show-overflow-tooltip />
      <el-table-column prop="category_name" label="分類" width="120" />
      <el-table-column label="類型" width="100">
        <template #default="{ row }">{{ typeLabels[row.item_type as Item['item_type']] }}</template>
      </el-table-column>
      <el-table-column label="單位" min-width="160">
        <template #default="{ row }">
          {{ row.base_unit_name }}
          <span v-for="u in row.units" :key="u.unit_id" class="muted">
            ・1{{ u.unit_name }}={{ Number(u.factor) }}{{ row.base_unit_name }}
          </span>
        </template>
      </el-table-column>
      <el-table-column prop="list_price" label="建議售價" width="110" align="right" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="80" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">編輯</el-button>
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
      v-model="visible"
      :title="editing ? '編輯料品' : '新增料品'"
      width="680px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="100px">
        <el-tabs v-model="dialogTab">
          <el-tab-pane label="基本資料" name="basic">
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="料號" prop="code" :rules="required" :error="fieldErrors.code">
                  <el-input v-model="form.code" maxlength="40" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="類型">
                  <el-radio-group v-model="form.item_type">
                    <el-radio v-for="(label, v) in typeLabels" :key="v" :value="v">{{
                      label
                    }}</el-radio>
                  </el-radio-group>
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="品名" prop="name" :rules="required" :error="fieldErrors.name">
              <el-input v-model="form.name" maxlength="200" />
            </el-form-item>
            <el-form-item label="規格" :error="fieldErrors.spec">
              <el-input v-model="form.spec" maxlength="255" />
            </el-form-item>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="分類" :error="fieldErrors.category_id">
                  <el-tree-select
                    v-model="form.category_id"
                    :data="categoryTree"
                    :props="{ label: 'name', children: 'children' }"
                    node-key="id"
                    check-strictly
                    clearable
                    style="width: 100%"
                  />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item
                  label="基本單位"
                  prop="base_unit_id"
                  :rules="required"
                  :error="fieldErrors.base_unit_id"
                >
                  <el-select v-model="form.base_unit_id" filterable style="width: 100%">
                    <el-option v-for="u in activeUnits" :key="u.id" :label="u.name" :value="u.id" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="條碼" :error="fieldErrors.barcode">
                  <el-input v-model="form.barcode" maxlength="50" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="稅別" :error="fieldErrors.tax_type_id">
                  <el-select v-model="form.tax_type_id" clearable style="width: 100%">
                    <el-option
                      v-for="t in taxTypes"
                      :key="t.id"
                      :label="t.name"
                      :value="t.id"
                      :disabled="!t.is_active"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item
                  label="建議售價"
                  prop="list_price"
                  :rules="decimalRule(6)"
                  :error="fieldErrors.list_price"
                >
                  <el-input v-model="form.list_price">
                    <template #append
                      >/{{ form.base_unit_id ? unitName(form.base_unit_id) : '單位' }}</template
                    >
                  </el-input>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item
                  label="安全庫存"
                  prop="safety_stock"
                  :rules="decimalRule(4)"
                  :error="fieldErrors.safety_stock"
                >
                  <el-input v-model="form.safety_stock" :disabled="form.item_type === 'service'" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="批號管理" :error="fieldErrors.lot_control">
              <el-radio-group v-model="form.lot_control" :disabled="form.item_type === 'service'">
                <el-radio v-for="(label, v) in lotControlLabels" :key="v" :value="v">{{
                  label
                }}</el-radio>
              </el-radio-group>
              <div class="hint">
                有庫存異動後不能再改。批號管理的料品入庫要輸入批號,出貨預設先到期先出。
              </div>
            </el-form-item>
            <el-form-item label="預設倉庫" :error="fieldErrors.default_warehouse_id">
              <el-select
                v-model="form.default_warehouse_id"
                clearable
                :disabled="form.item_type === 'service'"
                style="width: 100%"
              >
                <el-option
                  v-for="w in warehouses"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                  :disabled="!w.is_active"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="備註"
              ><el-input v-model="form.note" type="textarea" maxlength="2000"
            /></el-form-item>
            <el-form-item v-if="editing" label="啟用"
              ><el-switch v-model="form.is_active"
            /></el-form-item>
          </el-tab-pane>

          <el-tab-pane name="units">
            <template #label>
              單位換算
              <el-badge v-if="form.units.length" :value="form.units.length" type="info" />
            </template>
            <el-alert
              type="info"
              :closable="false"
              show-icon
              :title="`例:1 箱 = 12 ${form.base_unit_id ? unitName(form.base_unit_id) : '基本單位'}`"
              class="mb"
            />
            <el-alert
              v-if="fieldErrors.units"
              type="error"
              :title="fieldErrors.units"
              :closable="false"
              class="mb"
            />
            <el-table :data="form.units" border size="small">
              <el-table-column label="單位" width="160">
                <template #default="{ row, $index }">
                  <el-form-item
                    :error="fieldErrors[`units.${$index}`]"
                    class="cell-item"
                    label-width="0"
                  >
                    <el-select v-model="row.unit_id" size="small">
                      <el-option
                        v-for="u in activeUnits.filter((x) => x.id !== form.base_unit_id)"
                        :key="u.id"
                        :label="u.name"
                        :value="u.id"
                      />
                    </el-select>
                  </el-form-item>
                </template>
              </el-table-column>
              <el-table-column label="換算數量">
                <template #default="{ row, $index }">
                  <el-form-item
                    :prop="`units.${$index}.factor`"
                    :rules="[required, decimalRule(6, { positive: true })]"
                    class="cell-item"
                    label-width="0"
                  >
                    <el-input v-model="row.factor" size="small">
                      <template #prepend>=</template>
                      <template #append>{{
                        form.base_unit_id ? unitName(form.base_unit_id) : ''
                      }}</template>
                    </el-input>
                  </el-form-item>
                </template>
              </el-table-column>
              <el-table-column label="條碼" width="160">
                <template #default="{ row }">
                  <el-input v-model="row.barcode" size="small" maxlength="50" />
                </template>
              </el-table-column>
              <el-table-column width="60">
                <template #default="{ $index }">
                  <el-button link type="danger" @click="form.units.splice($index, 1)"
                    >刪除</el-button
                  >
                </template>
              </el-table-column>
            </el-table>
            <el-button class="mt" :disabled="!form.base_unit_id" @click="addUnitRow"
              >新增換算單位</el-button
            >
          </el-tab-pane>
        </el-tabs>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="dlg.save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.mb {
  margin-bottom: 12px;
}
.mt {
  margin-top: 8px;
}
.cell-item {
  margin-bottom: 0;
}
/* 表格內的錯誤訊息改為一般排版,撐高該列;預設絕對定位會被儲存格裁掉 */
.cell-item :deep(.el-form-item__error) {
  position: static;
  padding-top: 2px;
}
</style>
