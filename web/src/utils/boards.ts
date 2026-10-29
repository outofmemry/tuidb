// Pure helpers of the metrics boards (variable panels), tested without a browser.


type PanelLike = { Kind: string; var?: string; default?: string; options?: string[] | null }
const VARIABLE = new Set(['input', 'dropdown'])
export const isVarPanel = (p: { Kind: string }) => VARIABLE.has(p.Kind)

// panelVals gives every input / dropdown panel's variable its starting value: the panel's default, else the board variable's, else the first fixed option.
export function panelVals(vars: { Name: string; Default: string }[], panels: PanelLike[]): Record<string, string> {
  const out: Record<string, string> = Object.fromEntries(vars.map((v) => [v.Name, v.Default]))
  for (const p of panels) if (isVarPanel(p) && p.var) out[p.var] = p.default || out[p.var] || (p.options?.[0] ?? '')
  return out
}
