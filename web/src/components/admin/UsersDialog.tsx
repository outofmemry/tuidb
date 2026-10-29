// User admin: accounts, attributes, grants and resource limits.
import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type DbUser, type Plan, type SessionInfo } from '../../utils/api'
import { Dialog, ask } from '../shell/Dialogs'
import { enterSubmits, limitHint } from '../../utils/forms'
import { ACTIONS, ROW, TH, TD, msg } from '../shell/ui'

const PG_ATTRS = ['SUPERUSER', 'CREATEDB', 'CREATEROLE', 'LOGIN', 'REPLICATION', 'BYPASSRLS']
const PG_PRIVS = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']
const MY_PRIVS = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'ALTER', 'INDEX', 'ALL']

type Form = { kind: 'create' | 'password' | 'attrs' | 'grant' | 'revoke' | 'limits'; user?: DbUser }
// MySQL leaves Limits null when the max_* columns cannot be read; saving then would write zeros over the real quotas
const LIMITS_UNREADABLE = 'The resource limits of this account could not be read (the max_* columns of mysql.user are not visible to this login), so they cannot be changed here.'
const PG_LIMITS = [['CONNECTION_LIMIT', 'Connection limit']] as const // -1 = no limit
const MY_LIMITS = [['MAX_QUERIES_PER_HOUR', 'Queries per hour'], ['MAX_UPDATES_PER_HOUR', 'Updates per hour'], ['MAX_CONNECTIONS_PER_HOUR', 'Connections per hour'], ['MAX_USER_CONNECTIONS', 'Simultaneous connections']] as const

export function UsersDialog({ sid, info, onPlan, onClose }: { sid: string; info: SessionInfo; onPlan: (p: Plan) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['users', sid], queryFn: () => api<{ supported: boolean; users: DbUser[] }>('GET', `/sessions/${sid}/users`), gcTime: 0 })
  const [form, setForm] = useState<Form | null>(null)
  const [name, setName] = useState('')
  const [host, setHost] = useState('%')
  const [pw, setPw] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [object, setObject] = useState('')
  const [columns, setColumns] = useState('') // grant / revoke on just these columns (SELECT, INSERT, UPDATE, REFERENCES)
  const [limits, setLimits] = useState<Record<string, string>>({}) // MySQL resource limits; 0 = unlimited
  const [err, setErr] = useState('')
  const pg = info.driver === 'postgres'
  const lims = pg ? PG_LIMITS : MY_LIMITS
  const choices = pg ? PG_ATTRS : MY_PRIVS
  const open = (f: Form) => { setForm(f); setErr(f.kind === 'limits' && !pg && f.user && !f.user.Limits ? LIMITS_UNREADABLE : ''); setPw(''); setObject(''); setColumns(''); setPicked(f.kind === 'attrs' && pg ? f.user?.Attrs ?? [] : []); if (f.user) setName(f.user.Name); if (f.kind === 'limits') setLimits(Object.fromEntries(lims.map(([k]) => [k, String(f.user?.Limits?.[k] ?? (pg ? -1 : 0))]))) }
  const plan = async (body: Record<string, unknown>) => {
    setErr('')
    try { const p = await api<Plan>('POST', `/sessions/${sid}/users/plan`, body); onClose(); onPlan(p) } catch (e) { setErr(msg(e)) }
  }
  const who = (u: DbUser) => ({ Name: u.Name, Host: u.Host })
  const submit = () => {
    if (!form) return
    const u = form.user
    if (form.kind === 'create') return plan({ Kind: 'create', User: { Name: name, Host: pg ? '' : host, Attrs: pg ? picked : [] }, Password: pw })
    if (!u) return
    if (form.kind === 'password') return plan({ Kind: 'password', User: who(u), Password: pw })
    if (form.kind === 'limits') {
      if (!pg && !u.Limits) return setErr(LIMITS_UNREADABLE)
      const bad = lims.find(([k]) => !(pg ? /^(-1|\d+)$/ : /^\d+$/).test((limits[k] ?? '').trim()))
      if (bad) return setErr(`${bad[1]} must be a whole number (${limitHint(bad[0], pg)})`)
      return plan({ Kind: 'limits', User: { ...who(u), Limits: Object.fromEntries(lims.map(([k]) => [k, Number(limits[k])])) } })
    }
    if (form.kind === 'attrs') {
      // PostgreSQL turns a flag off with NO<flag>; MySQL only adds global privileges
      const had = u.Attrs ?? []
      const attrs = pg ? [...picked.filter((a) => !had.includes(a)), ...had.filter((a) => !picked.includes(a)).map((a) => 'NO' + a)] : picked
      return plan({ Kind: 'attrs', User: { ...who(u), Attrs: attrs } })
    }
    const cols = columns.split(',').map((c) => c.trim()).filter(Boolean)
    return plan({ Kind: form.kind, User: who(u), Grant: { Object: object, Privs: picked, Columns: cols.length ? cols : undefined } })
  }
  const drop = async (u: DbUser) => {
    const t = await ask(`Type ${u.Name} to drop this ${pg ? 'role' : 'account'}`)
    if (t === null) return
    if (t !== u.Name) return setErr('The name did not match; nothing dropped.')
    plan({ Kind: 'drop', User: who(u) })
  }
  const toggle = (a: string) => setPicked((p) => (p.includes(a) ? p.filter((x) => x !== a) : [...p, a]))
  const privs = form?.kind === 'grant' || form?.kind === 'revoke' ? (pg ? PG_PRIVS : MY_PRIVS) : choices
  return (
    <Dialog title="Users" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Loading…</div>}
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !q.data.supported && <div className="text-ink-2">User management is not supported by {info.driver}.</div>}
      {q.data?.supported && (
        <>
          <div className="overflow-auto max-h-[36vh] border border-rule">
            <table className="text-[12px] font-mono border-collapse w-full">
              <thead><tr>{['User', pg ? 'Attributes' : 'Global privileges', 'Grants', ''].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
              <tbody>{q.data.users.map((u) => (
                <tr key={u.Name + '@' + u.Host}>
                  <td className={`${TD} whitespace-nowrap`}>{u.Name}{u.Host && <span className="text-ink-2">@{u.Host}</span>}</td>
                  <td className={TD}>{(u.Attrs ?? []).join(', ')}</td>
                  <td className={TD}>{(u.Grants ?? []).map((g, k) => <div key={g.Object + k}>{g.Object}: <span className="text-ink-2">{g.Columns?.length ? g.Privs.map((x) => `${x} (${g.Columns!.join(', ')})`).join(', ') : g.Privs.join(', ')}</span></div>)}</td>
                  <td className={`${TD} whitespace-nowrap`}>
                    <button className="btn-ghost" onClick={() => open({ kind: 'password', user: u })}>Password</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'attrs', user: u })}>{pg ? 'Attributes' : 'Privileges'}</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'limits', user: u })}>Limits</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'grant', user: u })}>Grant</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'revoke', user: u })}>Revoke</button>
                    <button className="btn-ghost btn-danger" onClick={() => drop(u)}>Drop</button>
                  </td>
                </tr>))}</tbody>
            </table>
          </div>
          {!form && <div className={ACTIONS}><button onClick={() => open({ kind: 'create' })}>New user</button></div>}
          {form && (
            <div className="mt-3 border-t border-rule pt-2" role="group" aria-label="User form" onKeyDown={(e) => enterSubmits(e, submit)}>
              <strong className="font-medium">{form.kind === 'create' ? 'New user' : `${form.kind[0].toUpperCase() + form.kind.slice(1)}: ${form.user!.Name}`}</strong>
              {form.kind === 'create' && <div className={ROW}><input autoFocus placeholder="name" aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} />{!pg && <input placeholder="host" aria-label="Host" value={host} onChange={(e) => setHost(e.target.value)} />}</div>}
              {(form.kind === 'create' || form.kind === 'password') && <div className={ROW}><input type="password" placeholder="password" aria-label="Password" value={pw} onChange={(e) => setPw(e.target.value)} /></div>}
              {(form.kind === 'grant' || form.kind === 'revoke') && <div className={ROW}><input autoFocus className="flex-1" placeholder={pg ? 'schema.table' : 'db.table or db.*'} aria-label="Object" value={object} onChange={(e) => setObject(e.target.value)} /></div>}
              {(form.kind === 'grant' || form.kind === 'revoke') && <div className={ROW}><input className="flex-1" placeholder="columns, comma separated (optional: only for SELECT, INSERT, UPDATE, REFERENCES)" aria-label="Columns" value={columns} onChange={(e) => setColumns(e.target.value)} /></div>}
              {(form.kind === 'attrs' || form.kind === 'grant' || form.kind === 'revoke' || (form.kind === 'create' && pg)) && (
                <>
                  <div className="flex flex-wrap gap-x-4 gap-y-1 mt-2 text-[12.5px]" role="group" aria-label={pg && form.kind !== 'grant' && form.kind !== 'revoke' ? 'Attributes' : 'Privileges'}>{privs.map((a) => <label key={a} className="flex items-center gap-1.5"><input type="checkbox" checked={picked.includes(a)} onChange={() => toggle(a)} />{a}</label>)}</div>
                  <div className="flex gap-2 mb-2 mt-1"><button type="button" className="btn-ghost" onClick={() => setPicked([...privs])}>Check all</button><button type="button" className="btn-ghost" onClick={() => setPicked([])}>Uncheck all</button></div>
                </>
              )}
              {form.kind === 'limits' && (pg || form.user?.Limits) && <div className="grid grid-cols-[200px_1fr] gap-x-3 gap-y-2 items-center my-2 text-[12.5px]">
                {lims.map(([k, label], i) => <Fragment key={k}><label className="text-ink-2" htmlFor={`lim-${k}`}>{label}</label><input id={`lim-${k}`} autoFocus={i === 0} type="number" min={pg ? -1 : 0} inputMode="numeric" className="w-32" value={limits[k] ?? '0'} onChange={(e) => setLimits({ ...limits, [k]: e.target.value })} /></Fragment>)}
                <span /><span className="text-ink-3 text-[12px]">{lims.map(([k, label]) => `${label}: ${limitHint(k, pg)}`).join('. ')}.</span>
              </div>}
              {err && <div className="text-del">{err}</div>}
              <div className={ACTIONS}><button onClick={() => setForm(null)}>Cancel</button><button className="btn-primary" onClick={submit}>Preview</button></div>
            </div>
          )}
        </>
      )}
      {!form && err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}
