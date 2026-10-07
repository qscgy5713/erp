import { describe, expect, it } from 'vitest'
import { buildTree, descendantIds } from '@/utils/tree'

const items = [
  { id: 1, parent_id: null },
  { id: 2, parent_id: 1 },
  { id: 3, parent_id: 2 },
  { id: 4, parent_id: null },
  { id: 5, parent_id: 99 }, // 上層不存在 → 視為根
]

describe('tree', () => {
  it('buildTree', () => {
    const roots = buildTree(items)
    expect(roots.map((r) => r.id)).toEqual([1, 4, 5])
    expect(roots[0]!.children![0]!.children![0]!.id).toBe(3)
  })

  it('descendantIds 含自己與所有下層', () => {
    expect([...descendantIds(items, 1)].sort()).toEqual([1, 2, 3])
    expect([...descendantIds(items, 4)]).toEqual([4])
  })
})
