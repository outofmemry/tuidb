---
title: Configuration
weight: 14
---

webtuidb keeps its settings in plain TOML files under `~/.config/webtuidb` and everything it remembers between runs under `~/.local/share/webtuidb`. Both UIs read the same files, so a setting you change in the terminal also applies in the browser and the other way round. You can edit the files by hand or through the settings editor ({{< key "C-d ," >}} in both UIs).

{{< cards >}}
  {{< card link="config-file" title="The config file" subtitle="config.toml, where it lives, how defaults are layered, the settings editor, [keys] syntax." >}}
  {{< card link="settings-reference" title="Settings reference" subtitle="Every key of config.toml: type, default, allowed values, and which UI honours it." >}}
  {{< card link="connections-file" title="connections.toml" subtitle="Every connection field, per-driver options, SSH, dump tools, validation, extra connection folders." >}}
  {{< card link="themes" title="Themes" subtitle="Built-in palettes, colour overrides, theme files, installing themes from git." >}}
  {{< card link="key-bindings" title="Key bindings" subtitle="Rebinding actions, key name grammar, contexts, no-prefix keys, browser limits." >}}
  {{< card link="files-and-folders" title="Files and folders" subtitle="History, saved queries, state.json, boards, chats, plugins, crash log, locks." >}}
{{< /cards >}}
