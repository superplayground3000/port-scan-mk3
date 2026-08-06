# Issue tracker: GitHub

The issues and the PRDs of this repo are GitHub issues. Use the `gh` CLI for all operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`. Filter the comments with `jq`, and fetch the labels too.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with the correct `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Get the repo name from `git remote -v`. `gh` does this automatically inside a clone.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set this flag to `yes` if this repo treats external PRs as feature requests. `/triage` reads this flag.)_

At `yes`, PRs move through the same labels and states as issues, with the `gh pr` equivalents:

- **Read a PR**: `gh pr view <number> --comments` and `gh pr diff <number>` for the diff.
- **List external PRs for triage**: `gh pr list --state open --json number,title,body,labels,author,authorAssociation,comments`. Then keep only an `authorAssociation` of `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, or `NONE`. Drop `OWNER`, `MEMBER`, and `COLLABORATOR`.
- **Comment / label / close**: `gh pr comment`, `gh pr edit --add-label`/`--remove-label`, `gh pr close`.

GitHub shares one number space between issues and PRs, so a bare `#42` can be either one. Resolve it with `gh pr view 42`. If that command finds no PR, use `gh issue view 42`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`.

## Wayfinding operations

`/wayfinder` uses these operations. The **map** is a single issue, and the **child** issues are tickets.

- **Map**: a single issue with the label `wayfinder:map`. It holds the Notes / Decisions-so-far / Fog body. `gh issue create --label wayfinder:map`.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue (`gh api` on the sub-issues endpoint). If sub-issues are not enabled, add the child to a task list in the map body. Then put `Part of #<map>` at the top of the child body. Labels: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). After a claim, the ticket carries the driving dev as its assignee.
- **Blocking**: GitHub's **native issue dependencies** — the canonical representation, visible in the UI. Add an edge with `gh api --method POST repos/<owner>/<repo>/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`. `<blocker-db-id>` is the numeric **database id** of the blocker (`gh api repos/<owner>/<repo>/issues/<n> --jq .id`, _not_ the `#number` and _not_ the `node_id`). GitHub reports `issue_dependencies_summary.blocked_by` (open blockers only — the live gate). If dependencies are not available, use a `Blocked by: #<n>, #<n>` line at the top of the child body instead. A ticket is unblocked when every blocker is closed.
- **Frontier query**: list the open children of the map (`gh issue list --state open`, scoped to the sub-issues / task list of the map). Drop each child that has an open blocker (`issue_dependencies_summary.blocked_by > 0`, or an open issue in the `Blocked by` line) or an assignee. The first child in map order wins.
- **Claim**: `gh issue edit <n> --add-assignee @me` — the first write of the session.
- **Resolve**: `gh issue comment <n> --body "<answer>"`, then `gh issue close <n>`. Then append a context pointer (gist + link) to the Decisions-so-far list of the map.
