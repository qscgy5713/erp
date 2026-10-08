<script setup lang="ts">
// 會計科目表:依代號排序,下層科目縮排;彙總科目(群組)不可記帳
import { computed, onMounted, ref } from 'vue'
import { acctTypeLabels, glApi, type Account, type AcctType } from '@/api/gl'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['gl.account.write']))
const rows = ref<Account[]>([])
const keyword = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await glApi.accounts({ keyword: keyword.value.trim() })
  } catch (e) {
    dlg.handleError(e)
  } finally {
    loading.value = false
  }
}

/** 縮排層數:沿 parent_id 往上數 */
const depthOf = (a: Account): number => {
  let d = 0
  let cur: Account | undefined = a
  const byId = new Map(rows.value.map((r) => [r.id, r]))
  while (cur?.parent_id && d < 10) {
    cur = byId.get(cur.parent_id)
    d++
  }
  return d
}

const groups = computed(() => rows.value.filter((r) => !r.is_postable))

interface Form {
  code: string
  name: string
  acct_type: AcctType
  parent_id: number | null
  is_postable: boolean
  is_active: boolean
  note: string
}

const dlg = useFormDialog<Form, Account>({
  defaults: () => ({
    code: '',
    name: '',
    acct_type: 'asset',
    parent_id: null,
    is_postable: true,
    is_active: true,
    note: '',
  }),
  fromRow: (r) => ({
    code: r.code,
    name: r.name,
    acct_type: r.acct_type,
    parent_id: r.parent_id,
    is_postable: r.is_postable,
    is_active: r.is_active,
    note: r.note,
  }),
  create: (f) => glApi.createAccount(f),
  update: (id, f) => glApi.updateAccount(id, f),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-input
        v-model="keyword"
        placeholder="代號 / 名稱"
        clearable
        style="width: 200px"
        @keyup.enter="load"
        @clear="load"
      />
      <el-button @click="load">查詢</el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="dlg.openCreate()">新增科目</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" border size="small">
      <el-table-column label="代號" width="120">
        <template #default="{ row }">
          <span :style="{ paddingLeft: depthOf(row) * 16 + 'px' }">{{ row.code }}</span>
        </template>
      </el-table-column>
      <el-table-column label="名稱" min-width="200">
        <template #default="{ row }">
          <span :style="{ fontWeight: row.is_postable ? 'normal' : '600' }">{{ row.name }}</span>
          <el-tag v-if="!row.is_postable" size="small" class="ml">彙總</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="類別" width="90">
        <template #default="{ row }">{{ acctTypeLabels[row.acct_type as AcctType] }}</template>
      </el-table-column>
      <el-table-column label="上層" width="100" prop="parent_code" />
      <el-table-column label="狀態" width="90">
        <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
      </el-table-column>
      <el-table-column prop="note" label="備註" min-width="140" show-overflow-tooltip />
      <el-table-column v-if="canWrite" label="操作" width="80">
        <template #default="{ row }">
          <el-button link type="primary" @click="dlg.openEdit(row)">編輯</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="visible"
      :title="editing ? '編輯科目' : '新增科目'"
      width="480px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="90px" @submit.prevent="dlg.save">
        <el-form-item label="代號" prop="code" :rules="required" :error="fieldErrors.code">
          <el-input
            v-model="form.code"
            maxlength="10"
            :disabled="!!editing"
            placeholder="3–10 碼數字"
          />
        </el-form-item>
        <el-form-item label="名稱" prop="name" :rules="required" :error="fieldErrors.name">
          <el-input v-model="form.name" maxlength="100" />
        </el-form-item>
        <el-form-item label="類別" :error="fieldErrors.acct_type">
          <el-select
            v-model="form.acct_type"
            :disabled="!!editing?.has_entries"
            style="width: 100%"
          >
            <el-option v-for="(label, v) in acctTypeLabels" :key="v" :label="label" :value="v" />
          </el-select>
        </el-form-item>
        <el-form-item label="上層科目" :error="fieldErrors.parent_id">
          <el-select v-model="form.parent_id" clearable style="width: 100%">
            <el-option
              v-for="g in groups.filter(
                (x) => x.id !== editing?.id && x.acct_type === form.acct_type,
              )"
              :key="g.id"
              :label="`${g.code} ${g.name}`"
              :value="g.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="可記帳">
          <el-switch v-model="form.is_postable" :disabled="!!editing?.has_entries" />
          <span class="hint">關閉即為彙總科目(群組),不可直接記帳</span>
        </el-form-item>
        <el-form-item v-if="editing" label="啟用">
          <el-switch v-model="form.is_active" />
        </el-form-item>
        <el-form-item label="備註" :error="fieldErrors.note">
          <el-input v-model="form.note" maxlength="255" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="dlg.save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ml {
  margin-left: 6px;
}
.hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
