# Contributing to telexec

Thank you for your interest in contributing to `telexec`! We welcome contributions, bug reports, and suggestions from everyone.

## Code of Conduct

Please be respectful, constructive, and considerate of others when participating in discussions and development.

## Reporting Bugs

Before reporting a bug, check existing issues to ensure it hasn't already been reported.

When filing a bug report, please use our [Bug Report issue form](.github/ISSUE_TEMPLATE/bug_report.yml) and include:
- **Summary**: A clear, concise description of the bug.
- **Expected behavior**: What you expected to happen.
- **Observed behavior**: What actually happened.
- **Steps to reproduce**: Minimal step-by-step instructions or reproduction code.
- **Supporting details**:
  - Relevant logs, error output, or stack traces.
  - Environment details (OS, architecture, Go version, Nix version).

## Suggesting Features

If you have an idea for an improvement or new feature, please submit a [Feature Request](.github/ISSUE_TEMPLATE/feature_request.yml) describing the problem, proposed solution, and any alternatives considered.

## Submitting Pull Requests

We gladly review and accept improvements via Pull Requests.

### Development Workflow

This repository uses Nix Flakes for development and testing. Always run build and development commands within the Nix environment:

- **Run tests**:
  ```bash
  nix run .#test
  ```
- **Format code**:
  ```bash
  nix fmt
  ```
- **Regenerate mocks** (if modifying interfaces in `api.go`):
  ```bash
  nix run .#mock
  ```

### Guidelines for Contributions

1. **Branching**: Create a focused feature or bugfix branch from `master` (e.g. `fix-something` or `feat-something`).
2. **Simplicity is Critical**: Always aim for the simplest design that meets requirements. Avoid premature abstractions and unnecessary dependencies.
3. **Test-Driven Development (TDD)**:
   - Changes must be covered by automated tests.
   - Prefer table-driven unit tests (`TestXxx(t *testing.T)` with a slice of test cases).
   - Add integration tests where appropriate.
4. **Code Quality & Formatting**:
   - Run `nix fmt` before committing.
   - Ensure `nix run .#test` passes cleanly without warnings.
5. **Commits & PRs**:
   - Keep commits focused and logically structured with clear commit messages explaining *why* changes were made.
   - Open a Pull Request referencing the relevant issue number(s).
   - Complete the pre-merge checklist in the PR template.
