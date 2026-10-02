# Spreadsheet import format

An Admin can load the pilot's real Goals from a spreadsheet instead of entering
them by hand (ticket #22). The import reads **CSV** and **XLSX** (the first
worksheet of a workbook), one Goal per row, and turns each row into the same
actions a person would take in the UI. A CSV file may start with a UTF-8
byte-order mark, as Excel writes one when it saves "CSV UTF-8"; the import
ignores it.

An import runs in two steps:

1. **Dry run** — validates the file and reports errors row by row. It saves
   nothing.
2. **Commit** — imports every row in one transaction. It is **all-or-nothing**:
   if any row still has an error, the whole import is rolled back and nothing is
   saved.

A worked example is in [`testdata/import-example.csv`](../testdata/import-example.csv).

The Goal table's **Download CSV** link writes the Goals its filters keep in this
same format, with a leading `ID` column, so the file can be edited and imported
again to update those Goals' Dimension values and Fields (see
[Updating Goals by ID](#updating-goals-by-id)). A Goal carrying a value whose
name contains a semicolon, named so before that was refused, would not import
again, so such a download is refused, naming the Dimension and the value, and
no file is written; an Admin renames the value on the Dimensions page first.

## Columns

The first row is a header. Columns are matched by name, case-insensitively, in
any order. Any other column must name a [Dimension](../CONTEXT.md) or a
[Field](../CONTEXT.md) (see below); a column that names neither is rejected for
the whole file.

| Column          | Required | Meaning |
| --------------- | -------- | ------- |
| `ID`            | no       | An existing Goal's id. A row with one updates that Goal instead of creating one (see [Updating Goals by ID](#updating-goals-by-id)); a row without one creates a Goal. |
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

**No semicolons in values.** Because a semicolon always separates values, a
value can't contain one. There is no escape for it. The app refuses a value
containing a semicolon wherever one is added or renamed, so every value in a
list can be written in a cell; a cell such as `R&D; Ops` is read as the two
values `R&D` and `Ops`.

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

## Updating Goals by ID

A row whose `ID` cell holds a Goal's id updates that Goal rather than creating
one. It sets the Goal's value in each Dimension and Field column the file has,
exactly as the Goal page sets them, and every change is kept in the Goal's Value
history, credited to the Admin running the import:

- A Dimension cell becomes the Goal's whole set of values in that Dimension: a
  value it carried but the cell leaves out is removed, and an **empty cell
  clears** the Dimension. Values match, are added to an Extendable list, and are
  refused in a Fixed one as when creating a Goal. A Retired value the Goal
  already carries may stay, but can't be newly given.
- A Field cell becomes the Goal's value in that Field; an **empty cell clears**
  it.
- A value the Goal already has changes nothing and writes no history, so
  importing a download unchanged changes nothing.
- A Dimension or Field the file has no column for is left alone.

**Every other column on such a row is ignored**: `Title`, `Owner`, `So What`,
`Kind`, `Delivery Date`, `Milestones`, `Metrics` and `Parents` are not changed
by an import, and a row with an ID is never linked to a parent. When a row's cell
in one of those columns differs from its Goal, the result says once, for the
whole file, which columns were ignored.

The header still needs the required columns, since a row without an ID creates a
Goal as usual. A file may mix both kinds of row.

An `ID` that is not a number, matches no Goal, or appears on more than one row
is reported on that row. As with any row error, the whole file is still
validated, and a commit then saves nothing.

## What is reported

The dry run and a rolled-back commit report, per row: the row number, the Goal
title, and every error found on the row — a missing required field, a bad `Kind`
or date, a malformed Milestone or Metric, a `Parents` title that is not in the
file, a value not in a Fixed Dimension's list, more than one value in a
one-value Dimension (which says a value can't contain a semicolon), a Field number or date that doesn't parse, an `ID` that
matches no Goal, or a cycle. A row
with several problems lists them all in one dry run.

Rows are numbered as the spreadsheet numbers them: the header is row 1, so the
first Goal is row 2. In a CSV file a blank line still counts as a row, and a
quoted cell that spans several lines is still one row.
