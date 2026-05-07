# DSX Event Bus Monorepo

This repository contains the DSX Exchange event bus projects:

- `schema`
- `auth-callout`
- `deploy`
- `local`

The original source histories are preserved through merge import commits, and imported branch refs and tags are namespaced by source repository.

Examples:

```bash
git log --oneline --graph --all
git log --oneline -- auth-callout
git tag -l 'auth-callout/*'
git tag -l 'deploy/*'
```

The local evaluation environment uses the top-level `auth-callout` and `deploy` directories directly.
