## Watching?

## Epic Updates

- [x] Remove `Kube Security` as an epic, it is not meaningful
- [x] Remove `-` epic, we would rather unset
- [ ] Add `Client Go` as an epic
  - Update `Top Bets` to include this

## Revise author Field

- [ ] The `author` text field should have been `multi-select`, not
  a text string, with specific values matching github usernames, so
  humans don't typo if they change the value

## Watching field

- [ ] We need a `Watching` field as a multi-select, where:
    - I'm not `assigned`
    - I'm not the `author`
    - But I want to `watch` it to guide it if needed
    - Sadly, won't work with the `assignee:@me` but want something like this?
      - A watching field?
      - Problematic since you `save` views when changing values.
    - `Multi-Select Field` with our names set?

## Why did we miss?

- [ ] [Setup a website/docs](https://github.com/kubernetes-sigs/secrets-store-sync-controller/issues/9)
  - Assigned to `benjaminapetersen`
  - Why isn't this one on the board? It should be
- [ ] [kep-4317: add an in-cluster serving signer](https://github.com/kubernetes/enhancements/pull/5784)
  - Author is `stlaz`
  - Reviewers are `deads2k`, `micahhausler`
  - label has `sig/auth`
  - But no assignee

## Ideas?

- [ ] 2 weeks from tomorrow is `Kubernetes 1.36 Code Freeze`
  - This is a big deal for our team
  - I'm not sure we are tracking this well?
  - I don't know yet what to do to represent this better