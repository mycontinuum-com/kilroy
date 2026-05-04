## New things you can do

- **Run real Codex-backed attractors from this fork** — the OpenAI CLI path now uses the writable sandbox contract Kilroy needs for implementation stages, so runs can create code and status artifacts instead of stalling read-only.
- **Hide behavioral fixtures from implementation nodes with holdout globs** — directory and glob-based visibility controls now let reviews and QA see scenario packs without exposing them to ordinary implementation stages.
- **Install Kilroy as a pinned release artifact** — this fork now has a stable binary release surface that Serenity can provision directly, so operators do not need a local Go toolchain just to bootstrap the repo.

## Things that got better

- **Honor EU OpenAI endpoints in isolated Codex runs** — when `OPENAI_BASE_URL` is set, Kilroy now seeds isolated Codex config with that base URL so provider routing stays correct under run isolation.
- **Codex sandbox behavior is consistent across runtime, docs, and tests** — the release matches the live `danger-full-access` CLI contract instead of mixing old `workspace-write` guidance with newer runtime behavior.
- **Release publishing now targets the fork Serenity actually uses** — GitHub release output is configured for `mycontinuum-com/kilroy`, removing the stale upstream namespace assumption.
- **Holdout routing and scan behavior are easier to reason about** — path-based holdouts now cover the common exact-file, directory, and glob cases with clearer restoration semantics.
