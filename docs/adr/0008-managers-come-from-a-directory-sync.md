# Managers come from a directory sync, not from sign-in

A Chain (CONTEXT.md) needs every person's Manager, and the org's identity provider already knows it. The provider can hand it over two ways: as a `manager` claim when each person signs in, or through its directory API for everyone at once. We decided the tool reads Managers by syncing the directory, at startup and then hourly, with an Admin able to sync on demand. A claim only reaches the tool when its person signs in, so a leader's Chain would stay missing everyone below someone who hadn't. That includes Owners loaded by the spreadsheet import who never sign in themselves. A Chain that silently drops people is worse than none. A sync covers the whole org from the first run, and creates Accounts for people the tool hasn't seen, the way the import does.

## Consequences

- The tool depends on a directory API as well as on OpenID Connect sign-in. The first sync reads Authentik's users API; a different provider, or SCIM, is a new sync behind the same seam, and nothing above it changes.
- The directory is the only place a Manager is set. The tool never edits one (ADR 0002). It stores what the directory says, cycles included, and a Chain stops at anyone it has already reached.
- The sync never marks anyone Departed: Departed is recorded by an Admin, and a person missing from one sync is as likely a directory hiccup as a departure. A person the directory stops listing keeps their last-known Manager, so their Goals, Ownerless ones included, stay in the Chain where someone should notice them.
- The sync also sets each person's Name, which already comes from the org's sign-in. Sign-in still sets the Name of the person signing in.
- Without the sync configured there are no Managers, so there are no Chains, and the pages hide every choice that needs one.
- A Manager change is an attribute change like any other: a Chain finds different Goals from then on, and a Report rule on a Chain selects differently at its next draft (ADR 0007). Published Reports keep the snapshot they froze.
- If anyone proposes reading the `manager` claim at sign-in instead, point them to this ADR. Reading it as well, to update a person the moment they sign in, would not break it.
