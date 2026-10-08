<script setup lang="ts">
// 資料匯入:選類型 → 下載範本 → 上傳填好的 Excel → 預檢(列出所有錯誤)→ 確認匯入
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import { importsApi, type ImportBatch, type ImportResult, type ImportType } from '@/api/imports'
import { useApiError } from '@/composables/useApiError'
import { formatDateTime } from '@/utils/format'

const { handle } = useApiError()

const types = ref<ImportType[]>([])
const batches = ref<ImportBatch[]>([])
const typeKey = ref('')
const date = ref<string | null>(null)
const file = ref<File | null>(null)
const result = ref<ImportResult | null>(null)
const busy = ref<'check' | 'import' | null>(null)

const current = computed(() => types.value.find((t) => t.key === typeKey.value))
const groups = computed(() => [
  { label: '主檔', items: types.value.filter((t) => !t.needs_date) },
  { label: '期初資料', items: types.value.filter((t) => t.needs_date) },
])

async function load() {
  try {
    ;[types.value, batches.value] = await Promise.all([importsApi.types(), importsApi.batches()])
    if (!typeKey.value && types.value[0]) typeKey.value = types.value[0].key
  } catch (e) {
    handle(e)
  }
}

function onPick(f: UploadFile) {
  file.value = f.raw ?? null
  result.value = null
}

function reset() {
  file.value = null
  result.value = null
}

async function run(dryRun: boolean) {
  const t = current.value
  if (!t || !file.value) return
  if (t.needs_date && !date.value) {
    ElMessage.warning('請先選擇期初日期')
    return
  }
  if (!dryRun) {
    try {
      await ElMessageBox.confirm(
        `確定匯入「${t.label}」?${t.undoable ? '期初資料匯入後可在下方紀錄撤銷。' : '主檔匯入後無法整批撤銷,請確認預檢結果。'}`,
        '確認匯入',
        { type: 'warning', confirmButtonText: '匯入', cancelButtonText: '取消' },
      )
    } catch {
      return
    }
  }
  busy.value = dryRun ? 'check' : 'import'
  try {
    result.value = await importsApi.run(t.key, file.value, {
      dryRun,
      date: date.value ?? undefined,
    })
    if (!dryRun && result.value.ok) {
      ElMessage.success(`匯入完成:${result.value.summary}`)
      reset()
      batches.value = await importsApi.batches()
    }
  } catch (e) {
    handle(e)
  } finally {
    busy.value = null
  }
}

async function undo(b: ImportBatch) {
  try {
    await ElMessageBox.confirm(
      `撤銷「${b.type_label}」批次 ${b.id}(${b.summary})?期初庫存會以反向分錄沖銷,期初科目餘額會沖銷傳票;已被收付款沖帳或月結的資料無法撤銷。`,
      '撤銷匯入',
      { type: 'warning', confirmButtonText: '撤銷', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  try {
    await importsApi.undo(b.id)
    ElMessage.success('已撤銷')
    batches.value = await importsApi.batches()
  } catch (e) {
    handle(e)
  }
}

onMounted(load)
</script>

<template>
  <div>
    <el-card shadow="never" class="mb">
      <el-form label-width="90px">
        <el-form-item label="匯入類型">
          <el-select v-model="typeKey" style="width: 260px" @change="reset">
            <el-option-group v-for="g in groups" :key="g.label" :label="g.label">
              <el-option v-for="t in g.items" :key="t.key" :label="t.label" :value="t.key" />
            </el-option-group>
          </el-select>
          <el-button v-if="current" class="ml" @click="importsApi.template(current).catch(handle)">
            下載範本
          </el-button>
        </el-form-item>
        <el-form-item v-if="current" label="說明">
          <span class="hint">{{ current.desc }}</span>
        </el-form-item>
        <el-form-item v-if="current?.needs_date" label="期初日期">
          <el-date-picker v-model="date" value-format="YYYY-MM-DD" placeholder="例如上線前一天" />
          <span class="hint ml">所有期初金額都是這一天的餘額</span>
        </el-form-item>
        <el-form-item label="檔案">
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            accept=".xlsx"
            :on-change="onPick"
            :limit="1"
          >
            <el-button>選擇 Excel 檔(.xlsx)</el-button>
          </el-upload>
          <span v-if="file" class="ml">{{ file.name }}</span>
        </el-form-item>
        <el-form-item>
          <el-button :disabled="!file" :loading="busy === 'check'" @click="run(true)">
            預檢(不寫入)
          </el-button>
          <el-button
            type="primary"
            :disabled="!file || !result || !result.dry_run || !result.ok"
            :loading="busy === 'import'"
            @click="run(false)"
          >
            確認匯入
          </el-button>
          <span class="hint ml">請先預檢,通過後才能匯入;任何一列有錯,整批都不會寫入。</span>
        </el-form-item>
      </el-form>

      <template v-if="result">
        <el-alert
          :type="result.ok ? 'success' : 'error'"
          :closable="false"
          show-icon
          :title="
            result.ok
              ? `預檢通過:共 ${result.row_count} 列,${result.summary}`
              : `共 ${result.row_count} 列,發現 ${result.error_count} 個錯誤,請修正後重新上傳`
          "
        />
        <el-table
          v-if="!result.ok"
          :data="result.errors"
          border
          size="small"
          class="mt"
          max-height="360"
        >
          <el-table-column label="列" width="70" align="right">
            <template #default="{ row }">{{ row.row || '整體' }}</template>
          </el-table-column>
          <el-table-column prop="column" label="欄位" width="140" />
          <el-table-column prop="message" label="問題" min-width="320" />
        </el-table>
        <p v-if="result.error_count > result.errors.length" class="hint">
          只顯示前 {{ result.errors.length }} 個錯誤,修正後重新預檢可看到其餘。
        </p>
      </template>
    </el-card>

    <h3 class="sub">匯入紀錄</h3>
    <el-table :data="batches" border size="small">
      <el-table-column prop="id" label="批次" width="70" />
      <el-table-column prop="type_label" label="類型" width="130" />
      <el-table-column prop="filename" label="檔案" min-width="160" show-overflow-tooltip />
      <el-table-column prop="summary" label="結果" min-width="280" show-overflow-tooltip />
      <el-table-column label="時間" width="170">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="匯入者" width="110" />
      <el-table-column label="操作" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.undone_at" type="info">已撤銷</el-tag>
          <el-button v-else-if="row.undoable" link type="danger" @click="undo(row)">撤銷</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.mt {
  margin-top: 12px;
}
.ml {
  margin-left: 12px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sub {
  margin: 8px 0;
  font-size: 15px;
}
</style>
