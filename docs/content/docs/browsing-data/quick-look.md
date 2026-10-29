---
title: Quick look
description: Open one value in a viewer with raw, tree, pretty, minified and hex modes, image, PDF and geometry previews, and save or load the bytes.
weight: 60
---

{{< key "Enter" >}} on a cell opens quick look (`quick_look`): the full value in a scrollable pane instead of the truncated grid cell. In the browser the eye icon or Enter does the same.

Modes along the top of the viewer:

| Mode | Shows |
|---|---|
| Raw | the value as stored |
| Tree | a collapsible tree for JSON, arrays and documents |
| PRETTY / MINIFY | JSON re-indented or compacted (`[table] auto_pretty_json` does this in the grid itself) |
| Hex | bytes with offsets; binary cells can be edited here |
| View using encoding | re-decode text as UTF-8, Latin-1, UTF-16 and others |

Binary values are sniffed: images are previewed when `[table] image_preview` is on, PDFs open in the browser's viewer, and geometry columns show WKT and a small map when `[table] decode_spatial` is on. **Save** writes the bytes to a file and **Load** replaces them from one (a pending edit until committed). Very large values are capped in the viewer and offered as a download instead.

Copy from quick look copies the whole value, not the truncated cell text.
