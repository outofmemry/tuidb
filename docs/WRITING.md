# Writing guide for the webtuidb docs

The site is Hugo + the Hextra theme (vendored in `_vendor`). `hugo server -s docs` previews it,
`hugo -s docs --minify` builds it into `docs/public`. Search is FlexSearch over every page's content, so
pages must name features with the words a user would type (the action name, the key, the dialog title).

## Where things go

`content/docs/<section>/<page>.md`, one page per feature area. Front matter:

```yaml
---
title: Filter bar          # short, sentence case, what the user calls it
description: Narrow a table with one or more column conditions, in the terminal and in the browser.
weight: 30                 # order inside the section (10, 20, 30 …)
---
```

Section index pages (`_index.md`) hold a one-paragraph intro and a `{{< cards >}}` grid linking the pages.

## Every feature page covers both UIs

Both front ends run the same engine, so each page documents the feature once and shows how to reach it in
each UI. The pattern that works:

1. One or two sentences: what it is and when you want it.
2. A screenshot pair: `{{< ui tui="browsing-filter-bar" web="browsing-filter-bar" >}}Caption{{< /ui >}}`
   (file stems under `static/img/tui/` and `static/img/web/`, `.png` added; either side may be omitted).
3. **How to open it**: a short table or list, terminal keys first (`{{< key "C-d f" >}}`), then the web
   (the same prefix sequence works in the browser; also name the toolbar button, menu entry or right-click
   item). When a key cannot work in one UI, say so and give that UI's alternative.
4. **What you can do**: every control, option, operator, sub-dialog and keyboard shortcut inside the
   feature, in the order they appear. Tables for options. Mention the setting in `config.toml` that
   changes the default, in a "Settings" sub-section at the end, with a link to the configuration reference.
5. **Details and limits**: what happens on commit, what is logged, engine differences, anything marked
   `// shortcut:` in the code, and known limits. Use `{{< callout type="warning" >}}` for data-loss cases
   and `{{< callout type="info" >}}` for engine caveats.

Use `{{< key "…" >}}` for every key sequence. Key notation is the app's own: `C-d` prefix, `M-f` for Alt,
`S-Tab`, `Enter`, `Esc`, plain `j`. Write "prefix" only when explaining the concept; elsewhere show the
default `C-d`. Actions are referred to by their config name in backticks (`commit`, `filter_add_copy`) so
a reader can rebind them.

Hextra shortcodes you may use: `callout`, `cards`/`card`, `details`, `tabs`/`tab` (for engine-specific
variants), `steps`, `filetree`. Plain markdown tables for options. No HTML.

## Screenshots

Captured against the compose databases plus the demo "shop" dataset (`docs/tools/README.md`). Names are
`<section>-<feature>[-<state>].png`, lowercase, hyphenated, the same stem for the terminal and web shots of
the same state. The terminal default size is 150x42; use `-s 110x30` for a single dialog. The web default
viewport is 1440x900 dark. Every state a page describes that a reader could not picture from the text
gets a screenshot: the dialog open, the result after. Check the plain-text dump the terminal script prints
(and `--text` for the web) so the capture shows what the caption says.

Screenshots must come from the real app with the demo data; never mock, crop into something misleading,
or describe a state that was not captured.

## Voice

Second person, present tense, sentence case headings, short sentences. Say what happens, not what the
code does ("the row is struck through until you commit", not "the pending map stores the delete").
Name files, flags and settings exactly. When a behaviour differs between the terminal and the web, state
both in one sentence. No marketing adjectives.
