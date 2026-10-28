// The body of PUT /state/table. Fields left out are kept by the server, so a value the user never set is left out:
// the sort the PK default made (and the TUI's widths) must not be saved as if it had been chosen.
export function tableStateBody<S>(s: { hidden: string[]; filters: unknown; showBar: boolean; sort: S; sortChosen: boolean; widths: unknown; widthsChosen: boolean }) {
  return { hidden: s.hidden, filters: s.filters, show_filters: s.showBar, ...(s.sortChosen ? { sort: s.sort } : {}), ...(s.widthsChosen ? { widths: s.widths } : {}) }
}
