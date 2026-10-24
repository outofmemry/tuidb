# Web UI design notes

Concept: the connection's colour runs the room.

- **Accent:** once connected, the connection's own colour (config `color`) is the only accent in the
  workspace: the name badge in the top-left corner, the active tab underline, the selected row, the
  primary button, the primary-key marker, the focus ring. A red production database makes the whole
  app read as red. Connections without a colour get a monochrome chrome (`--accent` falls back to ink).
  Form fields keep an ivory focus ring so a red accent never reads as a validation error.
- **Surface:** graphite canvas, one step lighter for panels, hairlines for structure. No cards, no
  gradients, no borders on buttons. Dark is the default; light mode is cool stone, not cream.
- **Marks** never change hue: an edited cell is amber with an underline, a deleted row is struck
  through in coral, a new row carries a green number. The changeset tray lists every mark as old
  value struck through next to the new one, and nothing is sent until "Send changes".
- **Start page:** a switchboard. Connections set large, each beside its colour swatch and engine mark (simple-icons, drawn in ink, never brand colour), with the
  driver and host string in mono underneath. A paste-a-URL field is the first-run path.
- **Type:** Bricolage Grotesque for everything spoken, Martian Mono (narrow width axis) for data, the
  editor and connection strings. Sentence case throughout, no all-caps labels, no middle-dot joins.
- **Grid:** a ledger. Hairline column rules, sticky row numbers, the header one step lighter.
- **Quality floor:** visible keyboard focus, reduced motion respected, usable at phone width (the
  object list becomes a drawer, connection rows stack).
