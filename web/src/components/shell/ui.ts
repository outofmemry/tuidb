// Class names and helpers shared by the dialogs.
export const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
export const ROW = 'flex items-center gap-2.5 my-2 text-[12.5px]'
export const TH = 'text-left p-1 border-b border-rule-2 font-medium text-ink-2'
export const TD = 'p-1 border-b border-rule align-top'
export const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
