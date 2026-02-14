# Agent Guidelines for Dead Man's Switch

This doc provides guidelines for AI agents working on this codebase to maintain :sparkles: consistency :sparkles: and quality standards.

## Generated code

`api/gen.go` should never be modified by hand. `go generate ./...` will render changes in that file based on changes to `api/openapi.yaml`.


## Security

When changes are made, `gosec ./...` should pass without error. Should errors be encountered, fix them properly. Do not insert comments to ignore the issues.

## Linting

All code introduced should pass `golangci-lint run -v` without error. Should errors be encountered, fix them properly. Do not insert comments to ignore the issues.

## Go-Specific Syntax Rules

### Configuration and Code Organization

**Rule**: Configuration struct fields and CLI flag definitions must be alphabetically sorted by name.

**Good**:
```go
type Config struct {
	AutoTLS           bool
	ContactEmail      string
	DemoMode          bool
	Port              int
	StorageDir        string
	Validation        bool
}

serverFlags := []flagDef{
	{Name: autoTLSKey, ...},
	{Name: contactEmailKey, ...},
	{Name: demoModeKey, ...},
	{Name: portKey, ...},
	{Name: storageDirKey, ...},
	{Name: validationKey, ...},
}
```

**Bad**:
```go
type Config struct {
	ContactEmail      string
	AutoTLS           bool
	Validation        bool
	Port              int
	DemoMode          bool
	StorageDir        string
}
```

This ensures consistency across the codebase and makes it easier to find configuration options.

### Error Handling

**Rule**: Error handling conditions must always be placed on the next line after a statement that produces an error.

**Good**:
```go
result, err := someFunction()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

**Bad**:
```go
result, err := someFunction(); if err != nil {
    return fmt.Errorf("failed to do something: %w", err)
}
```

**Exception**: Only use `err == nil` checks when absolutely necessary (e.g., when explicitly verifying success), and these should be rare.

**Preferred**: Use the positive error check (`if err != nil`) pattern consistently throughout the codebase.

```go
// Good - positive error check
err := db.Init()
if err != nil {
    return fmt.Errorf("failed to initialize: %w", err)
}

// Avoid - negative error check (unless absolutely required)
if err == nil {
    // do something
}
```

## Testing Requirements

### Unit Test Coverage

**Rule**: Every new piece of functionality must have comprehensive unit tests that cover all cases and error scenarios.

**Requirements**:
1. **Happy Path**: Test the normal, expected behavior
2. **Error Cases**: Test all error conditions and edge cases
3. **Boundary Conditions**: Test limits and special values
4. **Return Values**: Verify all return values are correct

**Test Organization**:
- Place tests in `*_test.go` files alongside the functionality
- Use table-driven tests for multiple scenarios
- Name test cases descriptively with `TestFunctionName` convention
- Use subtests with `t.Run()` for organizing related tests

**Example**:
```go
func TestNewFeature(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr bool
	}{
		{
			name:     "valid input",
			input:    "test",
			expected: "result",
			expectErr: false,
		},
		{
			name:      "invalid input",
			input:     "",
			expected:  "",
			expectErr: true,
		},
		{
			name:      "edge case",
			input:     "edge",
			expected:  "edge_result",
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewFeature(tt.input)
			if (err != nil) != tt.expectErr {
				t.Errorf("unexpected error: got %v, wantErr %v", err, tt.expectErr)
			}
			if result != tt.expected {
				t.Errorf("unexpected result: got %v, want %v", result, tt.expected)
			}
		})
	}
}
```

**Coverage Expectations**:
- Aim for 80%+ code coverage on new functionality
- 100% coverage on error paths
- All branches should be tested

## Code Review Checklist

When creating new features, ensure:
- [ ] Error handling follows the next-line rule
- [ ] Unit tests exist for all code paths
- [ ] Tests cover happy path, error cases, and edge cases
- [ ] Code compiles without errors or warnings
- [ ] Existing tests still pass
