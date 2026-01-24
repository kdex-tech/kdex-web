# Dependency Management: libopenapi Compatibility Issue

## Problem Summary

The `libopenapi` ecosystem has a **breaking API change** between versions that causes build failures:

```
github.com/pb33f/libopenapi-validator@v0.10.2 is incompatible with libopenapi@v0.33.0
```

### Root Cause

- `libopenapi@v0.33.0` changed the hash function signature from `uint64` to `[32]byte`
- `libopenapi-validator@v0.10.2` expects the old `uint64` API
- `daveshanley/vacuum@v0.23.4` pulls in `libopenapi@v0.33.0`

This is a **semantic versioning violation** - breaking changes should bump the major version.

## Solution Applied

### 1. Explicit Version Pinning

Added to `go.mod` require block:

```go
require (
    // ... other dependencies ...
    
    // Pin to compatible versions to avoid breaking changes
    github.com/pb33f/libopenapi v0.31.2
    github.com/pb33f/libopenapi-validator v0.10.1
    
    // ... other dependencies ...
)
```

### 2. Enforcement Commands

```bash
go get github.com/pb33f/libopenapi@v0.31.2
go get github.com/pb33f/libopenapi-validator@v0.10.1
go mod tidy
```

This forces Go to use these specific versions even when transitive dependencies request newer ones.

## Long-Term Strategy

### Monitoring for Updates

**Watch for compatibility:**
1. Check https://github.com/pb33f/libopenapi-validator/releases
2. Look for `v0.10.3+` or `v0.11.0+` that supports `libopenapi@v0.33.0`
3. Test in a branch before updating

**When to update:**
```bash
# Test if new version is compatible
go get github.com/pb33f/libopenapi-validator@latest
go build ./cmd/main.go

# If successful, update the pin in go.mod
# If failed, wait for next release
```

### Prevention in CI/CD

Add to your CI pipeline:

```yaml
# .github/workflows/ci.yml
- name: Verify dependency compatibility
  run: |
    go mod verify
    go build ./cmd/main.go
```

### Dependency Update Policy

**Safe updates:**
- Patch versions (v0.10.1 → v0.10.2): Usually safe, test before merging
- Minor versions (v0.10.x → v0.11.x): Review changelog, test thoroughly

**Risky updates:**
- Major versions (v0.x → v1.x): Plan migration, expect breaking changes
- Transitive updates via `go get -u`: Can pull incompatible versions

**Recommended workflow:**
```bash
# Instead of: go get -u ./...
# Do selective updates:
go get -u github.com/specific/package@latest
go mod tidy
go build ./...
```

## Alternative Solutions

### Option A: Fork and Patch

If upstream is slow to fix:

```bash
# Fork libopenapi-validator
# Apply compatibility patch
# Use replace directive:
replace github.com/pb33f/libopenapi-validator => github.com/yourorg/libopenapi-validator v0.10.3-compat
```

### Option B: Remove vacuum Dependency

If you're not using OpenAPI validation features:

```bash
# Check what depends on vacuum
go mod graph | grep vacuum

# If nothing critical, remove it
go mod edit -droprequire github.com/daveshanley/vacuum
go mod tidy
```

### Option C: Use Dependabot with Constraints

In `.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    ignore:
      # Don't auto-update these until compatibility is confirmed
      - dependency-name: "github.com/pb33f/libopenapi"
        update-types: ["version-update:semver-minor"]
      - dependency-name: "github.com/pb33f/libopenapi-validator"
        update-types: ["version-update:semver-minor"]
```

## Current Status

✅ **Fixed:** Pinned to compatible versions  
✅ **Stable:** Build succeeds consistently  
⏳ **Monitoring:** Waiting for `libopenapi-validator@v0.10.3+`  

## When This Happens Again

1. **Identify the conflict:**
   ```bash
   go mod graph | grep libopenapi
   ```

2. **Find compatible versions:**
   - Check release notes
   - Test combinations
   - Use `go get package@version`

3. **Pin explicitly:**
   - Add to `require` block in `go.mod`
   - Add comment explaining why

4. **Document:**
   - Update this file
   - Note in commit message
   - Add to team knowledge base

## Related Issues

- https://github.com/pb33f/libopenapi/issues (check for breaking change discussions)
- https://github.com/pb33f/libopenapi-validator/issues (check for compatibility updates)
- https://github.com/daveshanley/vacuum/issues (check if they're aware)
