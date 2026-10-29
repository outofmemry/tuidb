---
title: Foreign keys
description: Follow a foreign key from a cell to the row it points to, and come back.
weight: 90
---

A column that references another table is marked in the header (the terminal shows a key glyph, the browser a link icon). On such a cell {{< key "C-d o" >}} (`follow_fk`) opens the referenced table in a new tab, filtered to the row the value points to, with the filter visible in the [filter bar](../filter-bar/) so you can widen it. In the browser the same action is in the cell menu and on the header icon.

Go back with the tab keys or the history actions in [Tabs and workspaces](../../workspace/tabs/). Composite keys follow all their columns; a NULL key has nowhere to go and the action is greyed out. The complete graph of a schema's keys is drawn by the [ER diagram](../../tools/er-diagram/).
