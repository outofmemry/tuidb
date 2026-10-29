# Web UI design notes

Styling is Tailwind v4 utility classes in the components. `web/src/styles.css` is the entry point:
it holds the palette tokens (surface, ink, marks, accent and the `--syn-*` syntax colours the SQL
editor reads through `components/query/cmtheme.ts`), element defaults, the CodeMirror hooks and a handful of
shared utilities (`btn-primary`, `btn-ghost`, `btn-danger`, `btn-on`, `btn-icon`, `popover`,
`menu-item`, `chip`, `toolbar`). Don't add new semantic classes there; put utilities on the element.

Concept: the connection's colour runs the room.

- **Accent:** once connected, the connection's own colour (config `color`) is the only accent in the
  workspace: the name badge in the top-left corner, the active tab underline, the selected row, the
  primary button, the primary-key marker, the focus ring. A red production database makes the whole
  app read as red. Connections without a colour fall back to the terminal UI's blue (`#7aa2f7`), so both front ends read as one.
  Form fields keep an ivory focus ring so a red accent never reads as a validation error.
- **Surface:** the terminal UI's Tokyo-Night palette: near-black navy canvas, one step lighter for panels,
  hairlines for structure. No cards, no gradients, no shadows inside the page. Buttons are hairline
  outlines, corners are 2px. Dark is the default; light mode is cool slate, not cream.
- **Marks** never change hue: an edited cell is amber with an underline, a deleted row is struck
  through in coral, a new row carries a green number. The changeset tray lists every mark as old
  value struck through next to the new one, and nothing is sent until "Send changes".
- **Start page:** a switchboard. Connections set large, each beside its colour swatch and engine mark (simple-icons, drawn in ink, never brand colour), with the
  driver and host string in mono underneath. A paste-a-URL field is the first-run path.
- **Type:** Martian Mono (narrow width axis) for the whole workspace chrome, data, the editor and
  connection strings, and for controls; Bricolage Grotesque only for prose (start page lede, dialog
  copy, empty states). The start page heading carries a blinking block cursor. Sentence case throughout, no all-caps labels, no middle-dot joins.
- **Grid:** a ledger. Hairline column rules, sticky row numbers, the header one step lighter. Columns
  start at the width of their name and first rows (a saved width wins), numbers sit flush right,
  NULL is dim italic.
- **Menus:** one popover style everywhere (`popover` + `menu-item`): the connection switcher, Select,
  the tab, row and ⋯ menus. Every floating menu renders through `Layer` (`components/shell/Layer.tsx`): a
  portal to `<body>` above dialogs and sticky headers, a backdrop that swallows the outside click (so
  nothing underneath is pressed by accident), and the same close rules everywhere: outside click,
  right-click outside, Escape, window blur or resize. Never hand-roll a `fixed inset-0` backdrop in a
  component. Less frequent actions go behind a ⋯ button rather than widening a toolbar.
  Motion is a short rise or pop, and `motion-safe:` keeps it off when the user asked for less.
- **Plain words first:** a failure says what to check in one sentence ("Nothing answered at that
  address…") and keeps the driver's text behind "Technical details". Anything that loses work asks
  first (closing or disconnecting with unsent changes).
- **Quality floor:** visible keyboard focus, reduced motion respected, usable at phone width (the
  object list becomes a drawer, connection rows stack).
