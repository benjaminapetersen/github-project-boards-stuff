# Tool Revisions

We collected too much of some things, missed other things,
and mis-categorized other things.  Time to revise!


## Top level Org filtering seems most incorrect

- `Azure` org, it looks like we collected far too many items.
  - Did our `sig-auth` filtering work?
  - Did our relevant users filtering work? `enj`,`aramase`,etc.
  - What might have gone wrong to cause so many additional items
    in this org to be collected, since many of them are not relevant
    to us at all?

## Newly Added

- How can we know what items we have confirmed are in the
  correct place on this board and have not yet been reviewed
  for this board? Do we need one more new column for
  `has been reviewed`?
  - This will be rather annoying if we are editing an item
    and it hops around. For example, changing the `epic`
    could cause a UI update and the item will be gone from
    view but we still need to set `has been reviewed`
  - We can likely mitigate with a `view` that is
    - Epics we care about as columns
    - Filtered down by `has not been reviewed`
      - Anything else doesn't need to show

## New View:

- "Have we manually seen and updated this before"?
  - With a simpler column name, of course.

## Revise

- `Azure/azure-workload-identity`
  - Most here will be about `workload identity`
    - Epic can be `-` none as we aren't working on this
  - Only some will be `identity bindings`
    - The few items relevant get this epic

- I removed lots of things from Epic `AI` and put them in
  Epic `-` as they are not relevant to us.  I want to make
  sure we don't override my manual changes and move them back.

- `Kube Security` is not an epic. It isn't a bucket to put
  stuff in. But it is one of our objectives, from which items
  from outside our normal Epic scope might need to show up
  on our radar.  So we should elimiate this epic, and put
  these items in `-`

- `Constrained Impersonation` epic is missing items, at least
  2 PRs known should be here.
  - [Tests parallel requests in constrained impersonation](https://github.com/kubernetes/kubernetes/pull/137339)
    by `qiujian16` and assigned to `enj`
  - [Add more unit tests for constrained impersonation](https://github.com/kubernetes/kubernetes/pull/136737)
    by `qiujian16` and assigned to `enj`

- `Pod certs` epic is missing items
  - Between `stlaz`, `ahmedtd` and one other person there is at
    least 1 PR.

- `SVM` is missing PRs from Michael
  - https://github.com/kubernetes/kubernetes/pull/135297 has
    `sig-auth` label.
  - https://github.com/kubernetes/kubernetes/pull/134762 has
    `sig-auth` label.

- Remove `ARO` epic, anything from ARO should have a sig-auth label
  or include one of our relevant users, and can just have `-` for the
  Epic, otherwise these should be removed.

- Remove `Dalec` epic, anything Dalec is build pipeline so really
  needs to go elsewhere.

- Why we have `No Epic` and also `-`, that likely is not helpful.

- `Image Pull Security` is entirely missing items, but `stlaz` is
  working on a PR right now.  Where is it?  Likely others.

## Broken

- Why `Azure/AKS` items on the board?

- Why is `Azure/ARO-HCP` included at all?
  - The Azure org should still only grab items with
    our team or sig labels?

- Why did it miss these items:
  - [Enable Writable cgroups](https://github.com/kubernetes/enhancements/issues/5474)
    - This should have been included due to `label:sig-auth`
      - We will put it under `AI epic`
  - [](https://github.com/kubernetes/kubernetes/pull/134947)
    - Should have `DRA`, and `enj` is reviewer
  - [](https://github.com/kubernetes/website/pull/54599)
    - Website docs update, but open by `ritazh` so I would expect
      this to show up.

- [](https://github.com/kubernetes/kubernetes/pull/137300)
  - by pmengelbert
  - for .kuberc
- [](https://github.com/kubernetes/kubernetes/pull/136354)
  - by pmengelbert
  - for .kuberc
- [](https://github.com/kubernetes/kubernetes/pull/137272)
  - by pmengelbert
  - merged, but should show up?
  - for .kuberc

- [](https://github.com/kubernetes/kubernetes/pull/137204)
  - by luxas
  - is for Conditional Authorization

- [](https://github.com/kubernetes/enhancements/pull/5784)
  - by stlaz
  - for the epic `Serving Certs for Kube Services`

## Suggested Path?

- If any of the users we care about are involved, we include it. Done.
  No further processing needed.
- If `sig/auth` or our desired labels on it, include it. Done.
  No further processing needed.



## Manually add?

- Draft issue to manually create for this since we can't see
  into the azure-manage-emtn-and-platforms Org:
- `Image Pull Security in AKS` should at least have the PRD?
  - The PR is in the EMU org, so maybe can't find it.
  - [This PR](https://github.com/azure-management-and-platforms/aks-handbook/pull/44)