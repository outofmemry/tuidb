// Drag and drop list helper.


// moveItem returns l with the element at from moved so it sits at index to.
export function moveItem<T>(l: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || from >= l.length) return l
  const o = l.filter((_, i) => i !== from)
  o.splice(Math.max(0, Math.min(o.length, to)), 0, l[from])
  return o
}
