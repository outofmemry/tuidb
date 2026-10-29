// Tree view of document values (MongoDB, Redis, DynamoDB).


export function Tree({ docs }: { docs: string[] }) {
  return <div className="flex-1 overflow-auto px-4 py-3 text-[12px] font-mono" data-ctx="tree">{docs.map((d, i) => { let v: unknown = d; try { v = JSON.parse(d) } catch { /* plain text */ } return <Node key={i} k={`#${i + 1}`} v={v} open /> })}</div>
}
function Node({ k, v, open }: { k: string; v: unknown; open?: boolean }) {
  if (v === null || typeof v !== 'object') return <div className="pl-4"><span className="text-ink-2">{k}: </span><span className={typeof v === 'string' ? 'text-new' : 'text-edit'}>{typeof v === 'string' ? JSON.stringify(v) : String(v)}</span></div>
  const entries = Object.entries(v as object)
  return (
    <details open={open} className="pl-4"><summary className="cursor-pointer"><span className="text-ink-2">{k} </span>{Array.isArray(v) ? `[${entries.length}]` : `{${entries.length}}`}</summary>
      {entries.map(([ek, ev]) => <Node key={ek} k={ek} v={ev} />)}</details>
  )
}
