# Cutting a release

Releases are driven by a tag. Everything else is done by CI.

## The steps

1. **Write the changelog.** Move what is under `## Unreleased` in
   [CHANGELOG.md](CHANGELOG.md) into a new `## vX.Y.Z` heading. The
   release notes on GitHub are taken from that section by the workflow,
   and a tag whose version has no section fails the build rather than
   publishing an empty release.
2. **Commit it**, on `main`.
3. **Tag and push:**

   ```
   git tag v0.1.0
   git push origin v0.1.0
   ```

4. CI runs the suite on Windows and on Linux. Only if both pass does it
   build the binaries, and put them on a GitHub release with the notes
   from the changelog.

## Numbering

[Semantic versioning](https://semver.org/spec/v2.0.0.html). While the
major version is 0 the shape is still moving, and a minor bump may
change how something behaves. `v1.0.0` is for when that stops being
true.

## What a release ships

- `gridterm_vX.Y.Z_windows_amd64.zip`
- `gridterm_vX.Y.Z_linux_amd64.tar.gz`
- `SHA256SUMS`

The version is stamped into the binary at link time, so a build can
always say which one it is: it is on the about dialog, it goes to a
program in a pane as `TERM_PROGRAM_VERSION`, and it goes to an agent
over MCP. A build from a working tree calls itself `dev-<commit>`, and
`dev-<commit>-dirty` when the tree had changes that are in no commit.

`make release` does the same thing by hand -- see
[BUILDING.md](BUILDING.md).

## What is not automated

- **macOS.** There is no build and no runner.
- **arm64.** Both releases are amd64.
- **Signing.** Neither binary is signed, so Windows will warn about an
  unknown publisher.
