<script setup lang="ts">
// 拋轉規則:業務單據過帳時,各分錄使用的會計科目。調整後只影響之後產生的傳票
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { glApi, type AccountOption, type Mapping } from '@/api/gl'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const auth = useAuthStore()
const canWrite = computed(() => auth.can(['gl.account.write']))
const { handle } = useApiError()

const rows = ref<Mapping[]>([])
const options = ref<AccountOption[]>([])
const loading = ref(false)
const saving = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    ;[rows.value, options.value] = await Promise.all([glApi.mappings(), glApi.accountOptions()])
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function change(row: Mapping, accountId: number) {
  saving.value = row.key
  try {
    await glApi.setMapping(row.key, accountId)
    ElMessage.success('已更新')
  } catch (e) {
    handle(e)
  } finally {
    saving.value = null
    await load()
  }
}

onMounted(load)
</script>

<template>
  <div>
    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb"
      title="進貨、出貨、收付款過帳時,系統依下表自動產生已過帳的傳票;調整只影響之後產生的傳票。銷貨成本於月結計算(M7)後拋轉。"
    />
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="label" label="用途" min-width="260" />
      <el-table-column label="會計科目" min-width="300">
        <template #default="{ row }">
          <el-select
            :model-value="row.account_id"
            filterable
            :disabled="!canWrite || saving === row.key"
            style="width: 100%"
            @change="(v: number) => change(row, v)"
          >
            <el-option
              v-for="o in options"
              :key="o.id"
              :label="`${o.code} ${o.name}`"
              :value="o.id"
            />
          </el-select>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
</style>
