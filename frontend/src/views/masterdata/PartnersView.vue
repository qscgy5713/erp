<script setup lang="ts">
// 客戶與供應商共用:kind 決定 API 與專屬欄位(客戶:發票抬頭、信用額度、負責業務;供應商:匯款資訊)
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  masterdataApi,
  type Address,
  type Contact,
  type Currency,
  type Customer,
  type PaymentTerm,
  type Supplier,
  type TaxType,
  type UserOption,
} from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { dataScopeLabels } from '@/utils/format'
import { decimalRule, required, taxIdRule } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const props = defineProps<{ kind: 'customer' | 'supplier' }>()
const isCustomer = computed(() => props.kind === 'customer')
const label = computed(() => (isCustomer.value ? '客戶' : '供應商'))
const auth = useAuthStore()
const canWrite = computed(() => auth.can(`masterdata.${props.kind}.write`))
// 信用額度屬風險控管,需另外的權限
const canSetCredit = computed(() => auth.can('masterdata.customer.credit'))

type Partner = Customer & Supplier

const api = computed(() =>
  isCustomer.value
    ? { list: masterdataApi.customers, get: masterdataApi.getCustomer }
    : { list: masterdataApi.suppliers, get: masterdataApi.getSupplier },
)

const query = reactive({ keyword: '', is_active: null as boolean | null, page: 1, size: 20 })
const rows = ref<Partner[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

const currencies = ref<Currency[]>([])
const taxTypes = ref<TaxType[]>([])
const terms = ref<PaymentTerm[]>([])
const users = ref<UserOption[]>([])

async function load() {
  loading.value = true
  try {
    const res = await api.value.list({ ...query, keyword: query.keyword.trim() })
    rows.value = res.items as Partner[]
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

interface PartnerForm {
  code: string
  name: string
  short_name: string
  tax_id: string
  invoice_title: string
  phone: string
  email: string
  contacts: Contact[]
  addresses: Address[]
  currency: string
  tax_type_id: number | null
  payment_term_id: number | null
  credit_limit: string
  sales_user_id: number | null
  bank_name: string
  bank_account: string
  note: string
  is_active: boolean
}

const defaults = (): PartnerForm => ({
  code: '',
  name: '',
  short_name: '',
  tax_id: '',
  invoice_title: '',
  phone: '',
  email: '',
  contacts: [],
  addresses: [],
  currency: 'TWD',
  tax_type_id: taxTypes.value.find((t) => t.code === 'TX5')?.id ?? null,
  payment_term_id: null,
  credit_limit: '0',
  // 本人範圍的業務只能建立自己負責的客戶,預設帶自己
  sales_user_id: isCustomer.value ? (auth.user?.id ?? null) : null,
  bank_name: '',
  bank_account: '',
  note: '',
  is_active: true,
})

function commonPayload(f: PartnerForm) {
  return {
    code: f.code,
    name: f.name,
    short_name: f.short_name,
    tax_id: f.tax_id.trim() || null,
    phone: f.phone,
    email: f.email,
    contacts: f.contacts.filter((c) => c.name.trim()),
    addresses: f.addresses.filter((a) => a.address.trim()),
    currency: f.currency,
    tax_type_id: f.tax_type_id,
    payment_term_id: f.payment_term_id,
    note: f.note,
    is_active: f.is_active,
  }
}

const customerPayload = (f: PartnerForm) => ({
  ...commonPayload(f),
  invoice_title: f.invoice_title,
  credit_limit: f.credit_limit,
  sales_user_id: f.sales_user_id,
})

const supplierPayload = (f: PartnerForm) => ({
  ...commonPayload(f),
  bank_name: f.bank_name,
  bank_account: f.bank_account,
})

const dlg = useFormDialog<PartnerForm, Partner>({
  defaults,
  fromRow: (r) => ({
    ...defaults(),
    ...r,
    tax_id: r.tax_id ?? '',
    contacts: r.contacts.map((c) => ({ ...c })),
    addresses: r.addresses.map((a) => ({ ...a })),
  }),
  create: (f) =>
    isCustomer.value
      ? masterdataApi.customer.create(customerPayload(f))
      : masterdataApi.supplier.create(supplierPayload(f)),
  update: (id, f) =>
    isCustomer.value
      ? masterdataApi.customer.update(id, { ...customerPayload(f), version: f.version })
      : masterdataApi.supplier.update(id, { ...supplierPayload(f), version: f.version }),
  onSaved: load,
})
const { visible, saving, editing, formRef, form, fieldErrors } = dlg
const dialogTab = ref('basic')

watch(fieldErrors, (errs) => {
  const keys = Object.keys(errs)
  if (keys.length && keys.every((k) => k.startsWith('contacts'))) dialogTab.value = 'contacts'
  else if (keys.length && keys.every((k) => k.startsWith('addresses')))
    dialogTab.value = 'addresses'
})

async function openEdit(row: Partner) {
  dialogTab.value = 'basic'
  try {
    // 重新讀取單筆,確保拿到最新版本
    dlg.openEdit((await api.value.get(row.id)) as Partner)
  } catch (e) {
    dlg.handleError(e)
  }
}

function openCreate() {
  dialogTab.value = 'basic'
  dlg.openCreate()
}

function setDefaultAddress(i: number) {
  form.addresses.forEach((a, idx) => (a.is_default = idx === i))
}

const scopeHint = computed(() =>
  isCustomer.value && auth.user && !auth.user.is_superadmin && auth.user.data_scope !== 'all'
    ? `你的資料範圍為「${dataScopeLabels[auth.user.data_scope]}」,只會看到負責業務在此範圍內的客戶`
    : '',
)

onMounted(async () => {
  load()
  try {
    ;[currencies.value, taxTypes.value, terms.value] = await Promise.all([
      masterdataApi.currencies(),
      masterdataApi.taxTypes(),
      masterdataApi.paymentTerms(),
    ])
    if (isCustomer.value) users.value = await masterdataApi.userOptions()
  } catch (e) {
    dlg.handleError(e)
  }
})
</script>

<template>
  <div>
    <el-alert
      v-if="scopeHint"
      :title="scopeHint"
      type="info"
      :closable="false"
      show-icon
      class="mb"
    />
    <div class="page-toolbar">
      <el-input
        v-model="query.keyword"
        placeholder="代碼、名稱、簡稱或統編"
        clearable
        style="width: 220px"
        @keyup.enter="search"
        @clear="search"
      />
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
      <el-button v-if="canWrite" type="primary" @click="openCreate">新增{{ label }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border>
      <el-table-column prop="code" label="代碼" width="120" />
      <el-table-column prop="name" label="名稱" min-width="180" />
      <el-table-column prop="short_name" label="簡稱" width="120" />
      <el-table-column prop="tax_id" label="統一編號" width="110" />
      <el-table-column prop="phone" label="電話" width="140" />
      <el-table-column prop="currency" label="幣別" width="70" />
      <el-table-column v-if="isCustomer" prop="sales_user_name" label="負責業務" width="110" />
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
      :title="`${editing ? '編輯' : '新增'}${label}`"
      width="760px"
      :close-on-click-modal="false"
    >
      <el-form ref="formRef" :model="form" label-width="100px">
        <el-tabs v-model="dialogTab">
          <el-tab-pane label="基本資料" name="basic">
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="代碼" prop="code" :rules="required" :error="fieldErrors.code">
                  <el-input v-model="form.code" maxlength="20" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="簡稱" :error="fieldErrors.short_name">
                  <el-input v-model="form.short_name" maxlength="50" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="名稱" prop="name" :rules="required" :error="fieldErrors.name">
              <el-input v-model="form.name" maxlength="200" />
            </el-form-item>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item
                  label="統一編號"
                  prop="tax_id"
                  :rules="taxIdRule"
                  :error="fieldErrors.tax_id"
                >
                  <el-input v-model="form.tax_id" maxlength="8" />
                </el-form-item>
              </el-col>
              <el-col v-if="isCustomer" :span="12">
                <el-form-item label="發票抬頭" :error="fieldErrors.invoice_title">
                  <el-input
                    v-model="form.invoice_title"
                    maxlength="200"
                    placeholder="空白則同名稱"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="12">
              <el-col :span="12">
                <el-form-item label="電話" :error="fieldErrors.phone">
                  <el-input v-model="form.phone" maxlength="50" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="Email" :error="fieldErrors.email">
                  <el-input v-model="form.email" maxlength="255" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="12">
              <el-col :span="8">
                <el-form-item label="幣別" :error="fieldErrors.currency">
                  <el-select v-model="form.currency" style="width: 100%">
                    <el-option
                      v-for="c in currencies"
                      :key="c.code"
                      :label="`${c.code} ${c.name}`"
                      :value="c.code"
                      :disabled="!c.is_active"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="8">
                <el-form-item label="稅別" label-width="60px" :error="fieldErrors.tax_type_id">
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
              <el-col :span="8">
                <el-form-item
                  label="付款條件"
                  label-width="80px"
                  :error="fieldErrors.payment_term_id"
                >
                  <el-select v-model="form.payment_term_id" clearable style="width: 100%">
                    <el-option
                      v-for="t in terms"
                      :key="t.id"
                      :label="t.name"
                      :value="t.id"
                      :disabled="!t.is_active"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row v-if="isCustomer" :gutter="12">
              <el-col :span="12">
                <el-form-item
                  label="信用額度"
                  prop="credit_limit"
                  :rules="decimalRule(4)"
                  :error="fieldErrors.credit_limit"
                >
                  <el-input v-model="form.credit_limit" :disabled="!canSetCredit">
                    <template #append>{{ canSetCredit ? '0 為不限' : '需信用額度權限' }}</template>
                  </el-input>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="負責業務" :error="fieldErrors.sales_user_id">
                  <el-select v-model="form.sales_user_id" filterable clearable style="width: 100%">
                    <el-option
                      v-for="u in users"
                      :key="u.id"
                      :label="`${u.name}(${u.username})`"
                      :value="u.id"
                    />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row v-else :gutter="12">
              <el-col :span="12">
                <el-form-item label="銀行" :error="fieldErrors.bank_name">
                  <el-input v-model="form.bank_name" maxlength="100" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="帳號" :error="fieldErrors.bank_account">
                  <el-input v-model="form.bank_account" maxlength="50" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="備註"
              ><el-input v-model="form.note" type="textarea" maxlength="2000"
            /></el-form-item>
            <el-form-item v-if="editing" label="啟用"
              ><el-switch v-model="form.is_active"
            /></el-form-item>
          </el-tab-pane>

          <el-tab-pane :label="`聯絡人(${form.contacts.length})`" name="contacts">
            <el-table :data="form.contacts" border size="small">
              <el-table-column label="姓名" width="140">
                <template #default="{ row }"
                  ><el-input v-model="row.name" size="small" maxlength="50"
                /></template>
              </el-table-column>
              <el-table-column label="職稱" width="120">
                <template #default="{ row }"
                  ><el-input v-model="row.title" size="small" maxlength="50"
                /></template>
              </el-table-column>
              <el-table-column label="電話" width="150">
                <template #default="{ row }"
                  ><el-input v-model="row.phone" size="small" maxlength="50"
                /></template>
              </el-table-column>
              <el-table-column label="Email">
                <template #default="{ row, $index }">
                  <el-form-item
                    :error="fieldErrors[`contacts.${$index}.email`]"
                    class="cell-item"
                    label-width="0"
                  >
                    <el-input v-model="row.email" size="small" maxlength="255" />
                  </el-form-item>
                </template>
              </el-table-column>
              <el-table-column width="60">
                <template #default="{ $index }">
                  <el-button link type="danger" @click="form.contacts.splice($index, 1)"
                    >刪除</el-button
                  >
                </template>
              </el-table-column>
            </el-table>
            <el-button
              class="mt"
              @click="form.contacts.push({ name: '', title: '', phone: '', email: '' })"
            >
              新增聯絡人
            </el-button>
          </el-tab-pane>

          <el-tab-pane :label="`地址(${form.addresses.length})`" name="addresses">
            <el-alert
              v-if="fieldErrors.addresses"
              type="error"
              :title="fieldErrors.addresses"
              :closable="false"
              class="mb"
            />
            <el-table :data="form.addresses" border size="small">
              <el-table-column label="標籤" width="110">
                <template #default="{ row }">
                  <el-input
                    v-model="row.label"
                    size="small"
                    maxlength="20"
                    placeholder="公司/送貨"
                  />
                </template>
              </el-table-column>
              <el-table-column label="郵遞區號" width="100">
                <template #default="{ row }"
                  ><el-input v-model="row.zip" size="small" maxlength="6"
                /></template>
              </el-table-column>
              <el-table-column label="地址">
                <template #default="{ row }"
                  ><el-input v-model="row.address" size="small" maxlength="255"
                /></template>
              </el-table-column>
              <el-table-column label="預設" width="70" align="center">
                <template #default="{ row, $index }">
                  <el-radio
                    :model-value="row.is_default"
                    :value="true"
                    @change="setDefaultAddress($index)"
                    >&nbsp;</el-radio
                  >
                </template>
              </el-table-column>
              <el-table-column width="60">
                <template #default="{ $index }">
                  <el-button link type="danger" @click="form.addresses.splice($index, 1)"
                    >刪除</el-button
                  >
                </template>
              </el-table-column>
            </el-table>
            <el-button
              class="mt"
              @click="
                form.addresses.push({
                  label: '',
                  zip: '',
                  address: '',
                  is_default: form.addresses.length === 0,
                })
              "
            >
              新增地址
            </el-button>
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
