import { masterdataApi, type Bin, type Warehouse } from '@/api/masterdata'

// 各倉庫是否啟用儲位與儲位清單的簡單快取(同一頁面多個明細列共用,避免每列都打 API)。
// 快取只活 30 秒:使用者剛啟用儲位或新增儲位後重新整理頁面就會看到最新的。
const TTL = 30_000
let warehouses: Warehouse[] | null = null
let warehousesAt = 0
let warehousesPromise: Promise<void> | null = null
const bins = new Map<number, { at: number; list: Promise<Bin[]> }>()

export function useBinStore() {
  async function ensureWarehouses() {
    if (warehouses && Date.now() - warehousesAt < TTL) return
    warehousesPromise ??= masterdataApi
      .warehouses()
      .then((w) => {
        warehouses = w
        warehousesAt = Date.now()
      })
      .finally(() => (warehousesPromise = null))
    await warehousesPromise
  }

  const usesBins = (id: number) => !!warehouses?.find((w) => w.id === id)?.use_bins

  function binsOf(id: number): Promise<Bin[]> {
    const hit = bins.get(id)
    if (hit && Date.now() - hit.at < TTL) return hit.list
    const list = masterdataApi.bins(id)
    bins.set(id, { at: Date.now(), list })
    list.catch(() => bins.delete(id))
    return list
  }

  return { ensureWarehouses, usesBins, binsOf }
}
