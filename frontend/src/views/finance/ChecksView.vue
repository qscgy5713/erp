<script setup lang="ts">
// 應收 / 應付票據:支票收付款過帳後登錄,到期兌現時轉入銀行存款(route meta.settle)
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { settlementApi, type CheckRow, type SettleSide } from '@/api/finance'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'

const route = useRoute()
const auth = useAuthStore()
const { handle } = useApiError()

const side = route.meta.settle as SettleSide
const canPost = computed(() => auth.can([`finance.${side}.post`]))
const money = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })
const status = ref<'pending' | 'cleared' | ''>('pending')
const rows = ref<CheckRow[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = await settlementApi.checks(side, { status: status.value || undefined })
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

async function act(row: CheckRow, action: 'clear' | 'unclear') {
  const label = action === 'clear' ? '兌現' : '取消兌現'
  try {
    await ElMessageBox.confirm(`票據 ${row.check_no || row.settlement_no} ${label}?`, label, {
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await settlementApi.checkAction(side, row.id, action)
    ElMessage.success(`已${label}`)
    await load()
  } catch (e) {
    handle(e)
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-radio-group v-model="status" @change="load">
        <el-radio-button value="pending">未兌現</el-radio-button>
        <el-radio-button value="cleared">已兌現</el-radio-button>
        <el-radio-button value="">全部</el-radio-button>
      </el-radio-group>
    </div>
    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="check_no" label="票號" width="160" />
      <el-table-column label="對象" min-width="180">
        <template #default="{ row }">{{ row.partner_code }} {{ row.partner_name }}</template>
      </el-table-column>
      <el-table-column label="收付款單" width="170">
        <template #default="{ row }">
          <router-link :to="`/finance/${side}s/${row.settlement_id}`">{{
            row.settlement_no
          }}</router-link>
        </template>
      </el-table-column>
      <el-table-column label="金額" width="130" align="right">
        <template #default="{ row }">{{ money(row.amount) }}</template>
      </el-table-column>
      <el-table-column label="到期日" width="140">
        <template #default="{ row }">
          {{ row.due_date }}
          <el-tag v-if="row.overdue" type="danger" size="small">已逾期</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="狀態" width="130">
        <template #default="{ row }">
          {{ row.status === 'cleared' ? `已兌現 ${row.cleared_date ?? ''}` : '未兌現' }}
        </template>
      </el-table-column>
      <el-table-column v-if="canPost" label="操作" width="120">
        <template #default="{ row }">
          <el-button v-if="row.status === 'pending'" link type="primary" @click="act(row, 'clear')"
            >兌現</el-button
          >
          <el-button v-else link @click="act(row, 'unclear')">取消兌現</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>
