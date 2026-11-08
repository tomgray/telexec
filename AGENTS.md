# Standing Instructions for Autonomous Agents

When working on this repository, all AI agents and automated coding assistants must adhere to the following rules:

1. **Simplicity is Critical**: Simplicity is critical. Always try to produce the simplest option that implements the user requirements, taking multiple iterations to simplify as needed. Avoid speculative generalizations, premature abstractions, and unnecessary dependencies.
2. **Code Readability & Responsibility**: Code must be clear, readable, and maintainable for a mix of agent and human contributors; humans are ultimately responsible for agent commits.
3. **Test-Driven Development (TDD)**: We must follow test-driven development. Our changes must be covered by tests. Write or update tests before or alongside implementation. In Golang, preference is for table-driven tests (`TestXxx(t *testing.T)` with a slice of test cases). Ideally create integration tests where appropriate.
4. **Pull Requests & Commits**: Always create a Pull Request for code changes. Create separate commits where the implementation plan has multiple parts, ensuring each commit represents a logical, coherent step.
5. **Clear Version Control History**: A clear version control history is helpful. Please minimise the code changes required (e.g. avoid moving blocks of code to a significantly different location, unless the user requests it) to keep commit history easy to follow.

## Development Environment & Tooling

This project uses the Nix build system with Nix Flakes (`flake.nix`). Always run development commands within the Nix environment:

- **Run tests**:
  ```bash
  nix develop -c go test -v ./...
  ```
- **Run formatting**:
  ```bash
  nix fmt
  ```
- **Regenerate mocks**:
  ```bash
  nix run .#mock
  ```
