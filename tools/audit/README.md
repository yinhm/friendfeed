# Group Audit Tool

This tool performs integrity checks on the Group-related data in the database to detect inconsistencies and data corruption.

## Usage

```bash
go run ./tools/audit -data /path/to/database
```

Or build and run:

```bash
go build -o audit ./tools/audit
./audit -data /srv/ffdb/data
```

## Checks Performed

### 1. Admin Non-Members
Detects GroupAdmin entries where the admin user is not a member of the group.
- **Issue**: Admin exists but corresponding Follow edge is missing
- **Expected**: Every admin must also be a member (Follow edge must exist)

### 2. Groups Without Admins
Detects groups that have no admin users.
- **Issue**: Group exists but has zero GroupAdmin entries
- **Expected**: Every group must have at least one admin

### 3. Orphaned Memberships
Detects Follow edges pointing to deleted or non-existent groups.
- **Issue**: User has membership to a deleted/missing group
- **Expected**: Follow edges should only point to active groups

### 4. Deleted Group Residuals
Detects GroupAdmin entries for groups that have been deleted.
- **Issue**: GroupAdmin entry exists for a deleted group
- **Expected**: When a group is deleted, all admin entries should be cleaned up

## Exit Code

- `0`: All checks passed, no issues found
- Non-zero: Issues detected (see log output)

## Example Output

```
2026/08/16 19:30:00 Opening database at /srv/ffdb/data
2026/08/16 19:30:00 Starting Group audit checks...
2026/08/16 19:30:00 Checking for admins who are not members...
2026/08/16 19:30:00   ✓ No orphaned admins found
2026/08/16 19:30:00 Checking for groups without admins...
2026/08/16 19:30:00   ✓ All groups have at least one admin
2026/08/16 19:30:00 Checking for orphaned memberships...
2026/08/16 19:30:00   ✓ No orphaned memberships found (checked 42 edges)
2026/08/16 19:30:00 Checking for deleted group residuals...
2026/08/16 19:30:00   ✓ No deleted group residuals found
2026/08/16 19:30:00 ✓ All audit checks passed
```

## When to Run

- After database migrations
- After implementing new group features
- Periodically as part of maintenance
- When investigating data consistency issues
- Before and after running data cleanup scripts
