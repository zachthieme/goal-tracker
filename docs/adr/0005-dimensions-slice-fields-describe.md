# Dimensions slice Goals, Fields describe them

Admin-defined attributes on a Goal come in two kinds. A Dimension's values come from a list, and it filters and groups Goals. A Field's value is entered directly on a Goal (a number, a short text, a long text, or a date), and it only describes that Goal. We rejected a single "custom field" concept with a type switch because the two behave differently everywhere they are used: a free-typed value has no list to pick from, makes one group per distinct value, and can't be a saved Report filter. One word for both would need a qualifier in every sentence. The test: if you would ever filter or group by it, it is a Dimension; if you would only read it on the Goal, it is a Field.

A Dimension's list is Fixed (only Admins add values) or Extendable (anyone setting a Goal's value can add one), and a Goal takes one value or several. These are two independent choices, not four kinds.

Numbers in a Field are never summed or averaged across Goals, for the reason ADR 0003 gives for Metrics: a Goal with several parents would be counted more than once.

## Consequences

- A Goal with several values in a Dimension appears under each of them when grouped, so group counts can add up to more than the number of Goals.
- A person-valued attribute (e.g. sponsor) is something you slice by, so it is an Extendable Dimension, not a Field.
- A Field is never an update. Long-text Fields stay off the Check-in form and out of a Report's narrative, so status keeps flowing through Check-ins.
- Dimensions, their values, and Fields are retired, never deleted, so Goals and saved Report Definitions that reference them keep working.
- If anyone asks for a budget total per group, point them to this ADR and ADR 0003.
