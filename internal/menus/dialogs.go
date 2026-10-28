package menus

// Dialog names one dialog the terminal opens for an action and the component the web opens for the same action
// (M14 check 6). TUI is a Go symbol (func, type or method name) or a distinctive title string in internal/ui; Web is a
// component name or a distinctive title string in web/src. parity_test.go checks both exist, so a dialog added to one UI
// without the other has to be listed here with its twin, or the test names the missing half.
type Dialog struct {
	Name string
	TUI  string
	Web  string
}

// Dialogs is every dialog pair. Options inside a dialog (Cascade, Restart identity...) are named in Name where a UI
// once lacked them.
var Dialogs = []Dialog{
	{"Set Value (NULL, DEFAULT, EMPTY, date, file, expression, sequence)", "func (a *App) pickSetValue(", "export function SetValueDialog("},
	{"Quick filter modes", "func (t *tableTab) quickFilter(", "function quickFilter("},
	{"Page settings (limit and offset)", "func (t *tableTab) pageSettings(", "export function PageDialog("},
	{"Count rows exactly", "func (t *tableTab) loadCountExact(", "count_exact: () => setExact("},
	{"Clone with content", "func (s *sidebar) cloneDialog(", "Duplicate the content too?"},
	{"Truncate options (Cascade, Restart identity)", "Restart identity", "Restart identity"},
	{"Delete / drop with the typed name", "Type its name", "Type its name to confirm"},
	{"Safe-mode gate with statements and password", "func (w *Workspace) gate(", "export function PlanDialog("},
	{"Safe-mode level picker", "func (w *Workspace) pickSafeMode(", "export function SafeModeDialog("},
	{"The server is taking too long", "taking too long", "taking too long"},
	{"Bootstrap command failure", "Bootstrap command failed", "Bootstrap command failed"},
	{"Import wizard", "func (w *Workspace) importWizard(", "export function ImportDialog("},
	{"Export", "func (w *Workspace) exportAction(", "export function ExportDialog("},
	{"Backup", "func (w *Workspace) backupAction(", "Back up database"},
	{"Restore", "func (w *Workspace) backupAction(", "Restore database"},
	{"Users", "func (w *Workspace) usersAction(", "export function UsersDialog("},
	{"Process list", "func (w *Workspace) processList(", "export function ProcessDialog("},
	{"Search in database", "Search in database", "SearchDialog"},
	{"Variables", "func (t *queryTab) editVars(", "export function VariablesDialog("},
	{"Find and replace", "func (t *queryTab) replaceInEditor(", "const findInEditor = "},
	{"Settings", "type settingsDlg struct", "export function SettingsDialog("},
	{"Keymap editor", "func (s *settingsDlg) editKey(", "function KeymapEditor("},
	{"Jobs centre", "type jobsDlg struct", "export function JobsDrawer("},
	{"Passcode lock", "type lockDlg struct", "export function Lock("},
	{"Assistant chat history", "Assistant chats", "function ChatHistory("},
	{"Foreign key form", "Add foreign key", "Add foreign key"},
	{"New database with engine options (encoding, collation, replication)", `Key: "collation"`, `aria-label="Collation"`},
	{"New table", "func (ws *Workspace) newTableDialog(", "New table"},
	{"Column header menu", "func (m colMenu) open(", "export function ItemMenu("},
	{"Open anything", "Open anything", "export function Palette("},
	{"Key bindings help", "type helpDlg struct", "function Help("},
}
