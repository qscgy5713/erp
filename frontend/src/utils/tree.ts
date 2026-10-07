type Flat = { id: number; parent_id: number | null }
export type WithChildren<T> = T & { children?: WithChildren<T>[] }

/** 由扁平的 {id, parent_id} 清單組成樹;找不到上層的節點視為根節點 */
export function buildTree<T extends Flat>(items: T[]): WithChildren<T>[] {
  const map = new Map<number, WithChildren<T>>()
  for (const it of items) map.set(it.id, { ...it })
  const roots: WithChildren<T>[] = []
  for (const node of map.values()) {
    const parent = node.parent_id !== null ? map.get(node.parent_id) : undefined
    if (parent) {
      ;(parent.children ??= []).push(node)
    } else {
      roots.push(node)
    }
  }
  return roots
}

/** 回傳 id 本身與所有子孫的 id(用於排除不可選為上層的部門) */
export function descendantIds(items: Flat[], id: number): Set<number> {
  const result = new Set<number>([id])
  let changed = true
  while (changed) {
    changed = false
    for (const it of items) {
      if (it.parent_id !== null && result.has(it.parent_id) && !result.has(it.id)) {
        result.add(it.id)
        changed = true
      }
    }
  }
  return result
}
