// The web side of internal/menus: GET /api/meta carries the lists, this evaluates each item's `when` predicates the way menus.Item.Holds does.
export type MenuItem = { label: string; action: string; when?: string; ui?: string; why?: string }
export type MenuEnv = { kind?: string; caps?: Record<string, unknown>; marked?: boolean; inGroup?: boolean; editable?: boolean }

export function holds(it: MenuItem, env: MenuEnv): boolean {
  for (const raw of (it.when ?? '').split(';')) {
    const p = raw.trim(), k = env.kind
    if (!p) continue
    if (p === 'tableish') { if (k !== 'table' && k !== 'view' && k !== 'matview') return false }
    else if (p === 'definition') { if (k !== 'view' && k !== 'matview' && k !== 'function' && k !== 'procedure') return false }
    else if (p === 'editable') { if (!env.editable) return false }
    else if (p === 'marked') { if (!env.marked) return false }
    else if (p === 'group') { if (!env.inGroup) return false }
    else if (p.startsWith('cap:')) { if (!env.caps?.[p.slice(4)]) return false }
    else if (k !== p) return false
  }
  return true
}

export const filterMenu = (list: MenuItem[] | undefined, env: MenuEnv): MenuItem[] => (list ?? []).filter((it) => holds(it, env))

// A menu entry the web cannot do is shown disabled with the list's reason (ui "tui"); one only the web does (ui "web") is shown as it is.
export const whyNot = (it: MenuItem): string | undefined => (it.ui === 'tui' ? it.why || 'terminal only' : undefined)
