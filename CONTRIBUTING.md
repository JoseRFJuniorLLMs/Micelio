# Contributing to Micelio

Thank you for your interest in contributing to the Micelio agent protocol. This document explains how to set up your development environment, run tests, and submit changes.

## Development Environment

### Prerequisites

- Go 1.22 or later
- Git
- (Optional) `golangci-lint` for linting
- (Optional) Docker for container builds

### Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/JoseRFJuniorLLMs/Micelio.git
   cd Micelio
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Verify the setup:
   ```bash
   make test
   ```

## Running Tests

Run the full test suite with race detection:

```bash
make test
```

Generate a coverage report:

```bash
make test-cover
# Open coverage.html in your browser
```

Run tests for a specific package:

```bash
go test -race -v ./pkg/identity/
go test -race -v ./pkg/protocol/
go test -race -v ./pkg/agent/
```

## Adding New Message Types

The AIP protocol uses typed messages defined in `pkg/protocol/`. To add a new message type:

1. Add the `MessageType` constant in `pkg/protocol/types.go`:
   ```go
   TypeMyNew MessageType = "MY_NEW"
   ```

2. Define the payload struct in `pkg/protocol/message.go`:
   ```go
   type MyNewPayload struct {
       Field string `json:"field"`
   }
   ```

3. Update the conversation FSM in `pkg/protocol/negotiation.go` if the new type participates in negotiation flows. Add valid transitions in `validateTransition()`.

4. Add handler support in `pkg/agent/agent.go` if the agent needs to react to the new type.

5. Write tests in `pkg/protocol/protocol_test.go` covering:
   - Message creation with `NewMessage()`
   - Encode/decode round-trip
   - FSM transitions involving the new type

## Commit Message Format

Use conventional commit messages:

```
type(scope): short description

Longer explanation if needed.
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `test`: Adding or updating tests
- `docs`: Documentation changes
- `refactor`: Code refactoring without behavior change
- `chore`: Build, CI, or tooling changes

Examples:
```
feat(protocol): add DELEGATE message type for task forwarding
fix(identity): handle nil seed in FromPrivateKey
test(agent): add concurrent capability registration test
```

## Pull Request Process

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```

2. Make your changes and ensure all tests pass:
   ```bash
   make all
   ```

3. Push your branch and open a pull request against `main`.

4. In the PR description, explain:
   - What the change does
   - Why it is needed
   - How it was tested

5. Address any review feedback. The CI pipeline must pass before merging.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use meaningful variable and function names
- Add doc comments to all exported types and functions
- Keep functions focused and short
- Handle errors explicitly; do not discard them

## Questions

If you have questions about contributing, open a GitHub issue or discussion.
