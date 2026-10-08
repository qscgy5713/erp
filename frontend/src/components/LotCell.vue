<script setup lang="ts">
// 明細列的批號 / 效期:
//   in  入庫(進貨、銷貨退回、調整增加):輸入批號(可從既有批號挑選),新批號另須效期;
//   out 出庫(出貨、進貨退出、調整減少、調撥):可指定批號,留空則先到期先出。
// 唯讀時顯示批號,已過帳的單據顯示實際異動的批號。
import { ref } from 'vue'
import { inventoryApi, type LotOption } from '@/api/inventory'
import { useApiError } from '@/composables/useApiError'

const props = defineProps<{
  itemId: number | null
  /** 料品的批號管理:lot 只要批號,lot_expiry 另要效期 */
  control?: string
  mode: 'in' | 'out'
  warehouseId?: number | null
  editable: boolean
  /** 已過帳:實際異動的批號 */
  lots?: { lot_no: string; expiry_date: string | null; qty: string }[]
}>()
const lotNo = defineModel<string>('lotNo', { default: '' })
const expiry = defineModel<string | null>('expiryDate', { default: null })

const { handle } = useApiError()
const options = ref<LotOption[]>([])
let loaded = false

async function load() {
  if (!props.itemId || (props.mode === 'out' && !props.warehouseId)) return
  try {
    options.value = await inventoryApi.lotOptions(
      props.itemId,
      props.mode === 'out' ? props.warehouseId : null,
    )
    loaded = true
  } catch (e) {
    handle(e)
  }
}

function label(o: LotOption): string {
  const exp = o.expiry_date ? ` 效期 ${o.expiry_date}` : ''
  const stock = o.qty !== undefined ? `(庫存 ${Number(o.qty).toLocaleString('zh-TW')})` : ''
  const flag =
    o.expiry_status === 'expired' ? ' 已過期' : o.expiry_status === 'expiring' ? ' 即將到期' : ''
  return `${o.lot_no}${exp}${stock}${flag}`
}

async function suggest(q: string, cb: (items: Record<string, string>[]) => void) {
  if (!loaded) await load()
  const kw = q.trim().toUpperCase()
  cb(
    options.value
      .filter((o) => o.lot_no.includes(kw))
      .map((o) => ({ value: o.lot_no, expiry: o.expiry_date ?? '' })),
  )
}

function onPick(item: Record<string, string>) {
  if (item.expiry) expiry.value = item.expiry
}

const qtyText = (v: string) => Number(v).toLocaleString('zh-TW')
</script>

<template>
  <template v-if="control && control !== 'none'">
    <template v-if="editable">
      <el-select
        v-if="mode === 'out'"
        v-model="lotNo"
        clearable
        size="small"
        placeholder="自動(先到期先出)"
        style="width: 100%"
        @visible-change="(v: boolean) => v && load()"
      >
        <el-option v-for="o in options" :key="o.lot_no" :value="o.lot_no" :label="label(o)" />
      </el-select>
      <template v-else>
        <el-autocomplete
          v-model="lotNo"
          :fetch-suggestions="suggest"
          size="small"
          placeholder="批號"
          style="width: 100%"
          @select="onPick"
        />
        <el-date-picker
          v-if="control === 'lot_expiry'"
          v-model="expiry"
          value-format="YYYY-MM-DD"
          size="small"
          placeholder="效期(新批號必填)"
          style="width: 100%; margin-top: 4px"
        />
      </template>
    </template>
    <template v-else>
      <div v-for="u in lots ?? []" :key="u.lot_no">
        {{ u.lot_no }} × {{ qtyText(u.qty) }}
        <span v-if="u.expiry_date" class="muted">效期 {{ u.expiry_date }}</span>
      </div>
      <div v-if="!lots?.length && lotNo">
        {{ lotNo }}
        <span v-if="expiry" class="muted">效期 {{ expiry }}</span>
      </div>
    </template>
  </template>
</template>

<style scoped>
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
