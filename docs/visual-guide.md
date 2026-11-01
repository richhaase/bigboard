# Bigboard workflow guide

[README](../README.md) · [User reference](reference.md)

## Choose the workspace

Sign in with GitHub, then open Settings from the app menu or with Command-comma.
In Repositories, manage GitHub access if needed, refresh the repository list,
and select what to collect. Sort by Recent push to find active repositories.
Apply saves the choices and closes Settings while data loads in the main window.
The status and timestamp at the bottom right form one clickable control for
coverage details. It spins during refresh, while progress also appears at the
top of the main screen.

Use the toolbar Repository filter to focus on a few already loaded repositories.
All loaded repositories start checked in one checklist. Uncheck the repositories
you want to hide; changes apply immediately while the picker stays open. The
last checked repository cannot be unchecked until you check another. Search
finds repositories, and **Select all loaded** restores every checkmark. Close and
reopen the picker to see the current selection. These are viewing changes only,
without refreshing data or saving collection settings.

## Follow a change through the code

1. Open **Activity → Heat**. Look for an area whose recent activity differs from
   its own baseline. Select Lines, Commits, or People according to the question.
2. Explore the repository, then a directory. Heat keeps 26 weeks of context;
   its Change badge compares four recent weeks with the preceding twelve.
3. Switch to **Directories** for measured additions/removals under the shared
   date range, then explore a file to see its commits.
4. Use **People** to inspect contributors to that scope and their commits.
5. Use **Related** to find shared commits or contributor overlap with other
   areas, then open the matching evidence.
6. Open repository PRs for current review/check context. **Back to Activity**
   returns to the code exploration and restores the normal PR filters.

Breadcrumbs and Back preserve your filters. Open commit links or a verified path
on GitHub when you need the source evidence. Line volume and overlap help locate
work; neither establishes ownership or productivity.

## Understand a contributor's activity

Choose **Contributors** and select a person. The detail shows commits,
repositories, active days, additions/removals, contribution rhythm, a daily
calendar, and recent commits. Select a repository under **Where work happened**
to continue in Activity.

The rhythm chart follows the selected evidence. The calendar uses a fixed
past-year grid; dates outside the selected range remain empty. Hovering a day
shows its date and commit count. Expand the shared date range when investigating
longer patterns.

If the same person appears under multiple email addresses, open **Identities**,
select the aliases and primary email, stage the rule, review, and save. The
mapping applies across collected history. Shared display names alone do not
merge contributors.

## Review pull requests

Choose **Pull requests** and use **Mine** to find your authored work, individual
review requests, or changes since your last known review. Combine it with Review,
Checks, State, and Author filters. Select a row to inspect the evidence before
opening GitHub.

Treat `+ ?` counts and incomplete coverage as missing evidence. Data Status
separates missing inventory from missing files or reviewers. GitHub remains the
place to review, comment, approve, or merge.

## Return after time away

Choose **Since last look** in the date menu once a bookmark exists. The baseline
stays fixed during your session, so refresh does not move the starting point.
A normal quit records eligible live snapshots for the next session.

Copy the current view as Markdown with `y`, or export all loaded contributor
history as JSON from Contributors. Exports do not make network requests.

For offline inspection, enable cached history in Settings → General. Use
Settings → Storage for eligible local cache removal. For incomplete or stale
results, click the bottom-right status and timestamp to inspect Data Status
before interpreting an empty view as no activity.
