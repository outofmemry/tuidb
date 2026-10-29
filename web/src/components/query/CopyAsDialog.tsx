// Copy as: pick the format to copy rows in.
import { COPY_FORMATS, type CopyFormat } from '../../utils/copy'
import { Dialog } from '../shell/Dialogs'

export function CopyAsDialog({ onPick, onClose }: { onPick: (f: CopyFormat) => void; onClose: () => void }) {
  return (
    <Dialog title="Copy as" onClose={onClose}>
      <div className="flex flex-col gap-0.5">
        {COPY_FORMATS.map((f, i) => <button key={f} autoFocus={i === 0} className="text-left border-transparent hover:border-ink-3" onClick={() => onPick(f)}>{f}</button>)}
      </div>
    </Dialog>
  )
}
