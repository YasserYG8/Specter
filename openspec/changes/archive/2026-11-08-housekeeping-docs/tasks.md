## 1. Go version

- [x] 1.1 In `openspec/config.yaml`, change `Go 1.24+` to `Go 1.26+`.
- [x] 1.2 In `PRODUCT.md`, change `Go 1.24+` to `Go 1.26+`.

## 2. Architecture draft

- [x] 2.1 Add the banner from the design at the top of `.agent/architecture.md`, under its title. Leave the rest unchanged.

## 3. CLAUDE.md

- [x] 3.1 Remove the "Known doc drift" section.
- [x] 3.2 In "Specs and workflow", make the `.agent/architecture.md` line say it holds historical design notes and that the code and `openspec/specs/` are the source of truth.

## 4. Check

- [x] 4.1 `grep -rn "1\.24" --include=*.md --include=*.yaml .` finds no Go version claims outside `openspec/changes/archive/`, and `openspec validate --specs --strict` still passes.
