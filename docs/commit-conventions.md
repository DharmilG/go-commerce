# Commit conventions

Every commit should explain a real change, not merely a category.

## Format

```text
<type>(<scope>): <specific change>

<why the change was needed, important tradeoffs, and verification>
```

The subject uses imperative mood and stays under 72 characters.

## Types

| Type | Use for |
|---|---|
| `feat` | New user-visible or API behavior |
| `fix` | A bug fix |
| `test` | Tests only |
| `docs` | Documentation only |
| `refactor` | Behavior-preserving restructuring |
| `perf` | Performance improvement |
| `build` | Docker, dependencies, build tooling |
| `ci` | CI configuration |
| `chore` | Maintenance that does not fit another type |

`chore` is allowed only when the subject is specific. A bare `chore`, `fix`, or
`update` commit is not acceptable.

## Scopes

Use the area being changed:

`auth`, `products`, `cart`, `orders`, `workers`, `db`, `http`, `config`,
`docker`, `tests`, `docs`, `opencode`, `ci`.

## Good examples

```text
feat(auth): add register and login endpoints with bcrypt and JWT

Passwords are hashed before insert and the login path uses the same error
for unknown emails and wrong passwords to avoid account enumeration.
```

```text
test(orders): prove concurrent buyers cannot oversell the last unit

Starts twenty checkouts against a product with stock 1 and asserts that
exactly one order is created and the final stock is 0.
```

```text
build(docker): publish Postgres on port 5434 because 5432 and 5433 are in use
```

```text
fix(cart): refresh the Redis TTL when the last item is removed

Deleting the final hash field previously left a key without an expiry.
```

```text
docs(api): document idempotent checkout replay and conflict responses
```

## Forbidden examples

```text
fix: bug
fix()
chore: updates
chore()
wip
changes
misc
```

## Rules

1. One logical change per commit.
2. Do not mix formatting, refactoring, and behavior in one commit.
3. Every commit must build and pass its tests.
4. Include the verification commands in the body when they are not obvious.
5. Do not commit `.env`, secrets, coverage output, or compiled binaries.
6. Never rewrite a commit message to hide what changed.
