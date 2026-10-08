<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { masterdataApi, type Bin, type Warehouse } from '@/api/masterdata'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('masterdata.warehouse.write'))
const rows = ref<Warehouse[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await masterdataApi.warehouses()
  } catch (e) {
    dlg.handleError(e)
  } finally {
    loading.value = false
  }
}

const dlg = useFormDialog({
  defaults: () => ({
    code: '',
    name: '',
    address: '',
    allow_negative: false,
    use_bins: false,
    is_active: true,
  }),
  fromRow: (r: Warehouse) => ({
    code: r.code,
    name: r.name,
    address: r.address,
    allow_negative: r.allow_negative,
    use_bins: r.use_bins,
    is_active: r.is_active,
  }),
  create: (f) => masterdataApi.warehouse.create(f),
  update: (id, f) => masterdataApi.warehouse.update(id, f),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg

// ---- 儲位管理 ----
const binDlg = ref(false)
const binWh = ref<Warehouse | null>(null)
const bins = ref<Bin[]>([])
const binForm = ref({ code: '', name: '' })
const binSaving = ref(false)

async function openBins(w: Warehouse) {
  binWh.value = w
  binForm.value = { code: '', name: '' }
  binDlg.value = true
  await loadBins()
}

async function loadBins() {
  if (!binWh.value) return
  try {
    bins.value = await masterdataApi.bins(binWh.value.id)
  } catch (e) {
    dlg.handleError(e)
  }
}

async function addBin() {
  if (!binWh.value || !binForm.value.code.trim()) return
  binSaving.value = true
  try {
    await masterdataApi.createBin({
      warehouse_id: binWh.value.id,
      code: binForm.value.code,
      name: binForm.value.name,
      is_active: true,
    })
    binForm.value = { code: '', name: '' }
    await loadBins()
  } catch (e) {
    dlg.handleError(e)
  } finally {
    binSaving.value = false
  }
}

async function toggleBin(b: Bin) {
  try {
    await masterdataApi.updateBin(b.id, {
      warehouse_id: b.warehouse_id,
      code: b.code,
      name: b.name,
      is_active: !b.is_active,
      version: b.version,
    })
    await loadBins()
  } catch (e) {
    dlg.handleError(e)
  }
}

async function removeBin(b: Bin) {
  try {
    await ElMessageBox.confirm(`刪除儲位 ${b.code}?`, '刪除儲位', {
      type: 'warning',
      confirmButtonText: '刪除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await masterdataApi.deleteBin(b.id)
    ElMessage.success('已刪除')
    await loadBins()
  } catch (e) {
    dlg.handleError(e)
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="dlg.openCreate()">新增倉庫</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="code" label="代碼" width="140" />
      <el-table-column prop="name" label="名稱" min-width="140" />
      <el-table-column prop="address" label="地址" min-width="220" show-overflow-tooltip />
      <el-table-column label="允許負庫存" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.allow_negative" type="warning">允許</el-tag>
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column label="儲位" width="90">
        <template #default="{ row }">
          <el-button v-if="row.use_bins" link type="primary" @click="openBins(row)">管理</el-button>
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
      </el-table-column>
      <el-table-column v-if="canWrite" label="操作" width="90">
        <template #default="{ row }">
          <el-button link type="primary" @click="dlg.openEdit(row)">編輯</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="visible"
      :title="editing ? '編輯倉庫' : '新增倉庫'"
      width="480px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="100px" @submit.prevent="dlg.save">
        <el-form-item label="代碼" prop="code" :rules="required" :error="fieldErrors.code">
          <el-input v-model="form.code" maxlength="20" />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :rules="required" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="地址" :error="fieldErrors.address">
          <el-input v-model="form.address" maxlength="255" />
        </el-form-item>
        <el-form-item label="允許負庫存">
          <el-switch v-model="form.allow_negative" />
          <span class="hint">預設不允許;開啟後出庫可超過現有量</span>
        </el-form-item>
        <el-form-item label="啟用儲位" :error="fieldErrors.use_bins">
          <el-switch v-model="form.use_bins" />
          <span class="hint">開啟後入庫須指定儲位;倉庫有庫存時不能切換</span>
        </el-form-item>
        <el-form-item v-if="editing" label="啟用"
          ><el-switch v-model="form.is_active"
        /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="dlg.save">儲存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="binDlg" :title="`儲位 — ${binWh?.code} ${binWh?.name}`" width="560px">
      <div v-if="canWrite" class="bin-add">
        <el-input
          v-model="binForm.code"
          placeholder="儲位代號(英數字,例如 A-01-02)"
          maxlength="20"
          style="width: 220px"
          @keyup.enter="addBin"
        />
        <el-input
          v-model="binForm.name"
          placeholder="名稱(選填)"
          maxlength="100"
          style="width: 180px"
          @keyup.enter="addBin"
        />
        <el-button type="primary" :loading="binSaving" @click="addBin">新增</el-button>
      </div>
      <el-table :data="bins" border size="small" max-height="360">
        <el-table-column prop="code" label="代號" width="140" />
        <el-table-column prop="name" label="名稱" min-width="140" />
        <el-table-column label="庫存合計" width="100" align="right">
          <template #default="{ row }">{{ row.stock_qty }}</template>
        </el-table-column>
        <el-table-column label="狀態" width="80">
          <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
        </el-table-column>
        <el-table-column v-if="canWrite" label="操作" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="toggleBin(row)">{{
              row.is_active ? '停用' : '啟用'
            }}</el-button>
            <el-button link type="danger" @click="removeBin(row)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<style scoped>
.bin-add {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
