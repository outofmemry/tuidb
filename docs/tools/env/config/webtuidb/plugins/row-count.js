// name: row-count
// key: alt+P
// contexts: grid, sidebar
// JavaScript plugin: counts the rows of the current table in a read-only transaction,
// copies the number and shows a notification.
const o = context.object
if (!o || !o.Name) throw new Error("open or select a table first")
const name = (o.Schema ? o.Schema + "." : "") + o.Name
const r = query("SELECT count(*) AS n FROM " + name)
const n = r.rows[0][0]
copy(n)
notify(name + ": " + n + " rows (copied)")
"Rows in " + name + ": " + n + "\nColumns returned: " + r.columns.join(", ")
