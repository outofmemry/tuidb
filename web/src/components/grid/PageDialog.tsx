// Row limit and offset of a table page.
import { useState } from 'react'
import { Dialog } from '../shell/Dialogs'
import { ACTIONS } from '../shell/ui'

export function PageDialog({ limit, offset, onApply, onClose }: { limit: number; offset: number; onApply: (limit: number, offset: number) => void; onClose: () => void }) {
  const [l, setL] = useState(String(limit)), [o, setO] = useState(String(offset))
  const ok = Number(l) > 0 && Number(o) >= 0 && Number.isInteger(Number(l)) && Number.isInteger(Number(o))
  return (
    <Dialog title="Row limit and offset" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (ok) onApply(Number(l), Number(o)) }}>
        <div className="flex items-center gap-2 my-2"><label className="w-24 text-ink-2">Limit</label><input autoFocus type="number" min={1} value={l} onChange={(e) => setL(e.target.value)} /></div>
        <div className="flex items-center gap-2 my-2"><label className="w-24 text-ink-2">Offset</label><input type="number" min={0} value={o} onChange={(e) => setO(e.target.value)} /></div>
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button className="btn-primary" type="submit" disabled={!ok}>Apply</button></div>
      </form>
    </Dialog>
  )
}
