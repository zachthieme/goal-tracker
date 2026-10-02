# Spreadsheet import format

An Admin can load the pilot's real Goals from a spreadsheet instead of entering
them by hand (ticket #22). The import reads **CSV** and **XLSX** (the first
worksheet of a workbook), one Goal per row, and turns each row into the same
actions a person would take in the UI.

An import runs in two steps:

1. **Dry run** — validates the file and reports errors row by row. It saves
   nothing.
2. **Commit** — imports every row in one transaction. It is **all-or-nothing**:
   if any row still has an error, the whole import is rolled back and nothing is
   saved.

A worked example is in [`testdata/import-example.csv`](../testdata/import-example.csv).

## Columns

The first row is a header. Columns are matched by name, case-insensitively, in
any order. Any other column must name a [Dimension](../CONTEXT.md) or a
[Field](../CONTEXT.md) (see below); a column that names neither is rejected for
the whole file.

| Column          | Required | Meaning |
| --------------- | -------- | ------- |
| `Title`         | yes      | The Goal's title. Titles must be unique within the file so that `Parents` can reference a Goal by title. |
| `Owner`         | yes      | The Owner's email address. If no account exists for it yet, one is created (people named in the file get accounts). |
| `So What`       | yes      | The customer problem the Goal addresses (CONTEXT.md: So What). |
| `Kind`          | yes      | `Dated` or `Ongoing`. |
| `Delivery Date` | for Dated | `YYYY-MM-DD`, or in XLSX a date cell (see below). Required when `Kind` is `Dated`; must be empty when `Kind` is `Ongoing`. |
| `Milestones`    | no       | Zero or more Milestones (see below). |
| `Metrics`       | no       | Zero or more Metrics (see below). |
| `Parents`       | no       | Zero or more parent Goal titles this Goal contributes to (see below). |
| _Dimension name_ | no      | Any column whose header matches a Dimension. The cell holds the value, or values, to give the Goal in that Dimension (see below). |
| _Field name_    | no       | Any column whose header matches a Field. The cell holds the Goal's value in that Field (see below). |

Imported Goals are created **Proposed**; they are not activated by the import.

### Dates in XLSX

Excel and Google Sheets usually turn a typed `2026-06-30` into a date cell,
which they then display in their own format (`06-30-26`, `30/06/2026`,
`Jul-26`, …). The import reads a date cell as its date, whatever its display
format, in any column — so a date-cell `Delivery Date` works as well as the text
`2026-06-30`. Milestones and Metrics are written as text entries (below), so
their dates are always `YYYY-MM-DD` text.

### Milestones

A semicolon-separated list; each entry is `Name @ YYYY-MM-DD`:

```
Design complete @ 2026-03-15; Beta @ 2026-05-01; GA @ 2026-06-15
```

### Metrics

A semicolon-separated list; each entry is six `|`-separated fields —
`Name | unit | up|down | baseline | target | YYYY-MM-DD`:

```
Activation rate | percent | up | 32 | 55 | 2026-06-30
```

`up` or `down` is the direction of desired movement; `baseline` and `target` are
numbers; the last field is the target date.

### Parents

A semicolon-separated list of other Goals' titles from the same file. A
`contributes to` link from this Goal (the child) to each named parent is created
and **accepted automatically**, because only an Admin runs an import (CONTEXT.md:
Admin can override links). A link that would close a **cycle** is rejected, and
because a commit is all-or-nothing the whole import is then rolled back.

### Dimensions

A column whose header matches a Dimension's name gives the Goal the cell's value
in that Dimension. The Dimension must already exist and must not be Retired; a
column naming an unknown or Retired Dimension is rejected for the whole file. A
blank cell assigns nothing.

A value matches the Dimension's list whatever its letter case or surrounding
spaces, so `acme` sets the existing `Acme`. A Retired value can't be newly
given, and is reported on that row.

**Several values.** In a Dimension that takes several values, a cell lists them
separated by semicolons; spaces around each are ignored:

```
Growth; Trust
```

In a Dimension that takes one value, a cell listing more than one is reported on
that row.

**Unknown values.** What happens to a value that isn't in the list depends on the
Dimension's list (CONTEXT.md: Fixed, Extendable):

- In an **Extendable** Dimension the value is added to the list, by the Admin
  running the import, and lands last in the list's order. A later row naming it,
  in any letter case, sets the same value.
- In a **Fixed** Dimension it is reported on that row; only an Admin adds to a
  Fixed list, from the Dimensions page.

Values are added only when the import commits: a dry run, or a commit rolled back
by any row's error, adds none.

### Fields

A column whose header matches a Field's name sets the Goal's value in that Field.
The Field must already exist and must not be Retired; a column naming an unknown
or Retired Field is rejected for the whole file. A blank cell sets nothing.

| Field type | Cell |
| ---------- | ---- |
| number     | A number, such as `1250000` or `2.5`, without its unit. One that doesn't parse is reported on that row. |
| date       | `YYYY-MM-DD`, or in XLSX a date cell. One that doesn't parse is reported on that row. |
| short text, long text | Any text. A long text may span several lines in a quoted CSV cell. |

## What is reported

The dry run and a rolled-back commit report, per row: the row number, the Goal
title, and every error found on the row — a missing required field, a bad `Kind`
or date, a malformed Milestone or Metric, a `Parents` title that is not in the
file, a value not in a Fixed Dimension's list, more than one value in a
one-value Dimension, a Field number or date that doesn't parse, or a cycle. A row
with several problems lists them all in one dry run.

Rows are numbered as the spreadsheet numbers them: the header is row 1, so the
first Goal is row 2. In a CSV file a blank line still counts as a row, and a
quoted cell that spans several lines is still one row.
